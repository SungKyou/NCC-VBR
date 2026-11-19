// 출처: ncc-xen backend/pkg/xapi/mixins/vm.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-03

package xapi

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// VMClient는 VM 관련 XAPI 메서드를 제공합니다.
type VMClient struct {
	c *Client
}

func NewVMClient(c *Client) *VMClient {
	return &VMClient{c: c}
}

// GetRecord는 VM ref로 전체 레코드를 반환합니다.
func (v *VMClient) GetRecord(ctx context.Context, vmRef string) (map[string]interface{}, error) {
	val, err := v.c.Call(ctx, "VM.get_record", v.c.SessionRef(), vmRef)
	if err != nil {
		return nil, fmt.Errorf("VM.get_record failed: %w", err)
	}
	return val.ToMap(), nil
}

// GetByUUID는 UUID로 VM ref를 반환합니다.
func (v *VMClient) GetByUUID(ctx context.Context, uuid string) (string, error) {
	val, err := v.c.Call(ctx, "VM.get_by_uuid", v.c.SessionRef(), uuid)
	if err != nil {
		return "", fmt.Errorf("VM.get_by_uuid failed (uuid=%s): %w", uuid, err)
	}
	return val.Str(), nil
}

// Snapshot은 VM UUID로 스냅샷을 생성합니다.
// Running 상태에서도 호출 가능 (crash-consistent).
func (v *VMClient) Snapshot(ctx context.Context, vmUUID, snapshotName string) (string, error) {
	vmRef, err := v.GetByUUID(ctx, vmUUID)
	if err != nil {
		return "", err
	}
	result, err := v.c.Call(ctx, "VM.snapshot", v.c.SessionRef(), vmRef, snapshotName)
	if err != nil {
		return "", fmt.Errorf("VM.snapshot failed: %w", err)
	}
	return result.Str(), nil
}

// DestroyByRef는 OpaqueRef로 VM을 삭제합니다 (롤백/임시 snapshot 정리용).
func (v *VMClient) DestroyByRef(ctx context.Context, vmRef string) error {
	_, err := v.c.Call(ctx, "VM.destroy", v.c.SessionRef(), vmRef)
	if err != nil {
		return fmt.Errorf("VM.destroy failed: %w", err)
	}
	return nil
}

func (v *VMClient) GetUUIDByRef(ctx context.Context, vmRef string) (string, error) {
	record, err := v.GetRecord(ctx, vmRef)
	if err != nil {
		return "", err
	}
	uuid, ok := record["uuid"].(string)
	if !ok || uuid == "" {
		return "", fmt.Errorf("uuid not found in VM record (ref=%s)", vmRef)
	}
	return uuid, nil
}

// TaskCreate는 진행률 추적용 XAPI Task를 수동 생성합니다.
// Async.* 호출은 Task가 자동 생성되지만, HTTP 핸들러(/export, /import 등)는
// task_id 쿼리 파라미터로 외부에서 만든 Task를 전달해야 진행률이 갱신됩니다.
// 수동 생성한 Task는 사용 후 TaskDestroy로 반드시 정리해야 합니다.
func (v *VMClient) TaskCreate(ctx context.Context, label, description string) (string, error) {
	result, err := v.c.Call(ctx, "task.create", v.c.SessionRef(), label, description)
	if err != nil {
		return "", fmt.Errorf("task.create failed: %w", err)
	}
	return result.Str(), nil
}

// TaskDestroy는 Task 레코드를 정리합니다.
// copy 완료/실패 후 반드시 defer로 호출해야 합니다.
func (v *VMClient) TaskDestroy(ctx context.Context, taskRef string) error {
	_, err := v.c.Call(ctx, "task.destroy", v.c.SessionRef(), taskRef)
	if err != nil {
		return fmt.Errorf("task.destroy failed: %w", err)
	}
	return nil
}

// TaskGetStatus는 Task의 현재 상태를 반환합니다.
// 반환값: "pending" | "success" | "failure" | "cancelling" | "cancelled"
func (v *VMClient) TaskGetStatus(ctx context.Context, taskRef string) (string, error) {
	val, err := v.c.Call(ctx, "task.get_status", v.c.SessionRef(), taskRef)
	if err != nil {
		return "", fmt.Errorf("task.get_status failed: %w", err)
	}
	return val.Str(), nil
}

// TaskGetProgress는 Task의 현재 진행률을 반환합니다 (0.0 ~ 1.0).
func (v *VMClient) TaskGetProgress(ctx context.Context, taskRef string) (float64, error) {
	val, err := v.c.Call(ctx, "task.get_progress", v.c.SessionRef(), taskRef)
	if err != nil {
		return 0, fmt.Errorf("task.get_progress failed: %w", err)
	}
	prog := val.Str()
	if prog == "" {
		return 0, nil
	}
	var f float64
	fmt.Sscanf(prog, "%f", &f)
	return f, nil
}

// TaskGetErrorInfo는 실패한 Task의 오류 정보를 반환합니다.
func (v *VMClient) TaskGetErrorInfo(ctx context.Context, taskRef string) (string, error) {
	val, err := v.c.Call(ctx, "task.get_error_info", v.c.SessionRef(), taskRef)
	if err != nil {
		return "", fmt.Errorf("task.get_error_info failed: %w", err)
	}
	slice := val.ToStringSlice()
	if len(slice) == 0 {
		return "알 수 없는 오류", nil
	}
	return strings.Join(slice, ": "), nil
}

// taskResultRefPattern은 task.get_result의 이중 인코딩된 XML 문자열에서
// OpaqueRef:xxx 형태의 값을 모두 추출합니다.
// (ncc-xen 원본에서는 TaskGetResultRefs 바로 위에 있던 변수다. 함수 단위 복사라 함께 옮겼다.)
var taskResultRefPattern = regexp.MustCompile(`OpaqueRef:[0-9a-fA-F-]+`)

// TaskGetResultRefs는 완료된 Task의 결과값에서 OpaqueRef 목록을 추출합니다.
// VM.import(비동기)는 XVA 내 VM 개수에 따라 단일 ref 또는 ref 배열을 반환하므로
// 기존 TaskGetResult(단일 ref 전용, VM.copy용)와 별도로 제공합니다.
// (2026-07-28: 실기테스트로 실제 응답 포맷 확인 후 정규식 확정)
func (v *VMClient) TaskGetResultRefs(ctx context.Context, taskRef string) ([]string, error) {
	val, err := v.c.Call(ctx, "task.get_result", v.c.SessionRef(), taskRef)
	if err != nil {
		return nil, fmt.Errorf("task.get_result query failed: %w", err)
	}
	raw := val.Str()
	matches := taskResultRefPattern.FindAllString(raw, -1)
	if len(matches) == 0 {
		return nil, nil
	}
	refs := make([]string, 0, len(matches))
	for _, m := range matches {
		if ref := strings.TrimSpace(m); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// SetIsATemplate은 VM.is_a_template 플래그를 설정합니다.
// 스냅샷 기반 XVA를 import하면 결과 VM이 is_a_template=true로 생성되는
// 알려진 XAPI 동작 때문에, restore 후 후처리로 false 전환이 필요합니다.
func (v *VMClient) SetIsATemplate(ctx context.Context, vmRef string, isTemplate bool) error {
	_, err := v.c.Call(ctx, "VM.set_is_a_template", v.c.SessionRef(), vmRef, isTemplate)
	if err != nil {
		return fmt.Errorf("VM.set_is_a_template failed: %w", err)
	}
	return nil
}

// UpdateAllowedOperations는 VM.allowed_operations 캐시 필드를 현재 상태 기준으로
// 재계산합니다 (🆕 2026-08-21 — zstd restore 실기테스트 중 발견: SetIsATemplate(false)로
// is_a_template 필드 자체는 바뀌어도 allowed_operations는 자동 재계산되지 않아
// "provision"(템플릿 전용 오퍼레이션)은 남아있고 "start"는 빠진 상태로 고정되는 XAPI 동작 확인.
// restore 후처리에서 SetIsATemplate 직후 반드시 호출해야 XenCenter Start 버튼이 정상 활성화됨.
// XAPI: VM.update_allowed_operations
func (v *VMClient) UpdateAllowedOperations(ctx context.Context, vmRef string) error {
	_, err := v.c.Call(ctx, "VM.update_allowed_operations", v.c.SessionRef(), vmRef)
	if err != nil {
		return fmt.Errorf("VM.update_allowed_operations failed: %w", err)
	}
	return nil
}
