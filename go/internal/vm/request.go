// Package vm 은 vbr_vm 의 업무다: 요청 검사, 작업 실행 순서, 상태 전이(docs/vm/03-tobe-design.md).
// HTTP 와 gin 을 모른다(처리기는 vm/api).
package vm

// request.go 의 요청 구조체는 ncc-xen backend/internal/job/handler.go Request·Payload(@043c6d5, CP-VM-01)를 따른다.
// 바뀐 부분(docs/vm/08-copied-code-register.md CP-VM-01):
//   - gin binding 태그 대신 Validate 로 필수 값을 검사하고 오류 코드·필드를 돌려준다(05 문서 9장).
//   - payload 에 credentials 를 더했다(DEC-VM-19).
//   - options 를 action 별 구조체로 해석한다.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"ncc-vbr/internal/redact"
	"ncc-vbr/internal/secret"
	"ncc-vbr/internal/xva"
)

// 작업 종류.
const (
	JobType       = "VM_BACKUP"
	ActionExport  = "export"
	ActionVerify  = "verify"
	ActionRestore = "restore"
)

// 스냅샷 정책(Q-VM-01).
const (
	SnapshotAuto   = "auto"
	SnapshotAlways = "always"
)

// Request 는 ncc-xen 규약 1 요청이다.
type Request struct {
	TaskID        string   `json:"taskId"`
	JobType       string   `json:"jobType"`
	Action        string   `json:"action"`
	ExecutionType string   `json:"executionType,omitempty"`
	SentAt        string   `json:"sentAt,omitempty"`
	Payload       *Payload `json:"payload"`
}

// Payload 는 규약 1 payload 에 credentials 를 더한 것이다.
type Payload struct {
	TargetType  string                 `json:"targetType"`
	TargetUUID  string                 `json:"targetUUID,omitempty"`
	MasterIP    string                 `json:"masterIp,omitempty"`
	PoolUUID    string                 `json:"poolUUID,omitempty"`
	Credentials *Credentials           `json:"credentials,omitempty"`
	Options     map[string]interface{} `json:"options"`
}

// Credentials 는 XAPI 계정이다. Password 는 ENC[...](crypto.enabled=false 면 평문도 된다).
type Credentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password"`
}

// ExportOptions 는 export options 다(05 문서 8.3).
type ExportOptions struct {
	FileName          string
	ExportPath        string
	Compression       string
	Disks             []int
	SnapshotMode      string
	SnapshotName      string
	KeepSnapshot      bool
	Overwrite         bool
	VerifyAfterExport *bool // nil 이면 설정 backup.verify_after_export
}

// RestoreOptions 는 restore options 다.
type RestoreOptions struct {
	FilePath            string
	SRUUID              string
	RestoreOriginalUUID bool
	Force               bool
	NameLabel           string
	HomeServerUUID      string
}

// VerifyOptions 는 verify options 다.
type VerifyOptions struct {
	FilePath string
}

// Job 은 검사를 마친 요청이다.
type Job struct {
	Req     Request
	Export  *ExportOptions
	Restore *RestoreOptions
	Verify  *VerifyOptions
}

var (
	taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	uuidPattern   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Validate 는 필수 값과 형식만 본다(지시 4: VM·SR·파일이 실제로 있는지는 보지 않는다).
// cryptoEnabled 면 평문 암호를 거부한다.
func Validate(r Request, cryptoEnabled bool) (*Job, *ReqError) {
	if r.TaskID == "" {
		return nil, missing("taskId")
	}
	if !taskIDPattern.MatchString(r.TaskID) {
		return nil, invalid("taskId", "taskId must be 1-64 characters of letters, digits, '-' or '_'")
	}
	if r.JobType == "" {
		return nil, missing("jobType")
	}
	if r.Action == "" {
		return nil, missing("action")
	}
	if r.JobType != JobType {
		return nil, &ReqError{Code: CodeUnsupportedJob, Field: "jobType", Msg: "unsupported jobType: " + r.JobType}
	}
	if r.ExecutionType != "" && r.ExecutionType != "async" {
		return nil, invalid("executionType", "executionType must be \"async\"")
	}
	if r.Payload == nil {
		return nil, missing("payload")
	}
	p := r.Payload
	if p.TargetType == "" {
		return nil, missing("payload.targetType")
	}
	if p.Options == nil {
		return nil, missing("payload.options")
	}
	job := &Job{Req: r}
	var err *ReqError
	switch r.Action {
	case ActionExport:
		err = checkTarget(p, "VM", true)
		if err == nil {
			err = checkPool(p, cryptoEnabled)
		}
		if err == nil {
			job.Export, err = parseExport(p.Options)
		}
	case ActionRestore:
		err = checkTarget(p, "FILE", false)
		if err == nil {
			err = checkPool(p, cryptoEnabled)
		}
		if err == nil {
			job.Restore, err = parseRestore(p.Options)
		}
	case ActionVerify:
		err = checkTarget(p, "FILE", false)
		if err == nil {
			job.Verify, err = parseVerify(p.Options)
		}
	default:
		return nil, &ReqError{Code: CodeUnsupportedJob, Field: "action", Msg: "unsupported action: " + r.Action}
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}

func checkTarget(p *Payload, want string, needUUID bool) *ReqError {
	if p.TargetType != want {
		return &ReqError{Code: CodeUnsupportedJob, Field: "payload.targetType",
			Msg: fmt.Sprintf("targetType must be %s for this action", want)}
	}
	if !needUUID {
		return nil
	}
	if p.TargetUUID == "" {
		return missing("payload.targetUUID")
	}
	if !uuidPattern.MatchString(p.TargetUUID) {
		return invalid("payload.targetUUID", "targetUUID is not a UUID")
	}
	return nil
}

func checkPool(p *Payload, cryptoEnabled bool) *ReqError {
	if strings.TrimSpace(p.MasterIP) == "" {
		return missing("payload.masterIp")
	}
	if p.Credentials == nil || p.Credentials.Password == "" {
		return missing("payload.credentials.password")
	}
	if cryptoEnabled && !secret.HasEnvelopePrefix(p.Credentials.Password) {
		return &ReqError{Code: CodePlaintextPassword, Field: "payload.credentials.password",
			Msg: "plaintext password is not allowed while crypto.enabled is true"}
	}
	return nil
}

func parseExport(o map[string]interface{}) (*ExportOptions, *ReqError) {
	e := &ExportOptions{SnapshotMode: SnapshotAuto, Compression: xva.CompressionNone}
	var err *ReqError
	if e.FileName, err = strField(o, "file_name", true); err != nil {
		return nil, err
	}
	if !xva.ValidFileName(e.FileName) {
		return nil, invalid("payload.options.file_name", `file_name must not contain \ / : * ? " < > | or ".."`)
	}
	if e.ExportPath, err = strField(o, "export_path", false); err != nil {
		return nil, err
	}
	if c, err := strField(o, "compression", false); err != nil {
		return nil, err
	} else if c != "" {
		e.Compression = strings.ToLower(c)
	}
	switch e.Compression {
	case xva.CompressionNone, xva.CompressionGzip, xva.CompressionZstd:
	default:
		return nil, invalid("payload.options.compression", "compression must be none, gzip or zstd")
	}
	if m, err := strField(o, "snapshot_mode", false); err != nil {
		return nil, err
	} else if m != "" {
		e.SnapshotMode = strings.ToLower(m)
	}
	if e.SnapshotMode != SnapshotAuto && e.SnapshotMode != SnapshotAlways {
		return nil, invalid("payload.options.snapshot_mode", "snapshot_mode must be auto or always")
	}
	if e.SnapshotName, err = strField(o, "snapshot_name", false); err != nil {
		return nil, err
	}
	if e.Disks, err = disksField(o); err != nil {
		return nil, err
	}
	if e.KeepSnapshot, err = boolField(o, "keep_snapshot"); err != nil {
		return nil, err
	}
	if e.Overwrite, err = boolField(o, "overwrite"); err != nil {
		return nil, err
	}
	if _, ok := o["verify_after_export"]; ok {
		v, err := boolField(o, "verify_after_export")
		if err != nil {
			return nil, err
		}
		e.VerifyAfterExport = &v
	}
	return e, nil
}

func parseRestore(o map[string]interface{}) (*RestoreOptions, *ReqError) {
	r := &RestoreOptions{}
	var err *ReqError
	if r.FilePath, err = strField(o, "file_path", true); err != nil {
		return nil, err
	}
	if r.SRUUID, err = strField(o, "sr_uuid", true); err != nil {
		return nil, err
	}
	if !uuidPattern.MatchString(r.SRUUID) {
		return nil, invalid("payload.options.sr_uuid", "sr_uuid is not a UUID")
	}
	if r.RestoreOriginalUUID, err = boolField(o, "restore_original_uuid"); err != nil {
		return nil, err
	}
	if r.Force, err = boolField(o, "force"); err != nil {
		return nil, err
	}
	if r.NameLabel, err = strField(o, "name_label", false); err != nil {
		return nil, err
	}
	if r.HomeServerUUID, err = strField(o, "home_server_uuid", false); err != nil {
		return nil, err
	}
	if r.HomeServerUUID != "" && !uuidPattern.MatchString(r.HomeServerUUID) {
		return nil, invalid("payload.options.home_server_uuid", "home_server_uuid is not a UUID")
	}
	return r, nil
}

func parseVerify(o map[string]interface{}) (*VerifyOptions, *ReqError) {
	p, err := strField(o, "file_path", true)
	if err != nil {
		return nil, err
	}
	return &VerifyOptions{FilePath: p}, nil
}

func strField(o map[string]interface{}, key string, required bool) (string, *ReqError) {
	field := "payload.options." + key
	v, ok := o[key]
	if !ok || v == nil {
		if required {
			return "", missing(field)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", invalid(field, key+" must be a string")
	}
	s = strings.TrimSpace(s)
	if s == "" && required {
		return "", missing(field)
	}
	return s, nil
}

// boolField 는 bool 또는 "true"/"false" 문자열을 받는다(ncc-xen boolOpt 와 같다).
func boolField(o map[string]interface{}, key string) (bool, *ReqError) {
	v, ok := o[key]
	if !ok || v == nil {
		return false, nil
	}
	switch t := v.(type) {
	case bool:
		return t, nil
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(t))
		if err == nil {
			return b, nil
		}
	}
	return false, invalid("payload.options."+key, key+" must be a boolean")
}

func disksField(o map[string]interface{}) ([]int, *ReqError) {
	v, ok := o["disks"]
	if !ok || v == nil {
		return nil, nil
	}
	list, ok := v.([]interface{})
	if !ok || len(list) == 0 {
		return nil, invalid("payload.options.disks", "disks must be a non-empty array of disk numbers")
	}
	seen := map[int]bool{}
	var out []int
	for _, x := range list {
		f, ok := x.(float64)
		if !ok || f < 0 || f != float64(int(f)) {
			return nil, invalid("payload.options.disks", "disks must contain non-negative integers")
		}
		n := int(f)
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// Redacted 는 기록·로그용 요청 사본(JSON)이다. 암호를 가린다. 계정 이름은 남긴다.
func Redacted(r Request) json.RawMessage {
	b, err := json.Marshal(r)
	if err != nil {
		return nil
	}
	var tree map[string]interface{}
	if json.Unmarshal(b, &tree) != nil {
		return nil
	}
	if p, ok := tree["payload"].(map[string]interface{}); ok {
		if c, ok := p["credentials"].(map[string]interface{}); ok {
			cc := map[string]interface{}{"password": redact.Mask}
			if u, ok := c["username"]; ok {
				cc["username"] = u
			}
			p["credentials"] = cc
		}
		if o, ok := p["options"]; ok {
			p["options"] = redact.Value(o)
		}
	}
	out, err := json.Marshal(tree)
	if err != nil {
		return nil
	}
	return out
}
