package vm

import (
	"errors"

	"ncc-vbr/internal/xapi"
	"ncc-vbr/internal/xva"
)

// 요청 단계 오류 코드(05 문서 9장, HTTP 응답).
const (
	CodeInvalidRequest    = "INVALID_REQUEST"
	CodeMissingField      = "MISSING_FIELD"
	CodeInvalidField      = "INVALID_FIELD"
	CodeUnsupportedJob    = "UNSUPPORTED_JOB"
	CodePlaintextPassword = "PLAINTEXT_PASSWORD_REJECTED"
	CodeTaskNotFound      = "TASK_NOT_FOUND"
	CodeAlreadyFinished   = "TASK_ALREADY_FINISHED"
	CodeQueueFull         = "QUEUE_FULL"
	CodeStopping          = "SERVICE_STOPPING"
	CodeStoreError        = "STORE_ERROR"
)

// 작업 단계 오류 코드(05 문서 9장, 작업 상태 error.code). xva 패키지 코드와 같은 값을 쓴다.
const (
	CodeDecryptFailed    = "CREDENTIAL_DECRYPT_FAILED"
	CodeXAPIConnect      = "XAPI_CONNECT_FAILED"
	CodeXAPIAuth         = "XAPI_AUTH_FAILED"
	CodeXAPI             = xva.CodeXAPI
	CodeDiskNotFound     = "DISK_NOT_FOUND"
	CodeVMState          = "VM_STATE_UNSUPPORTED"
	CodePathNotAllowed   = xva.CodePathNotAllowed
	CodeFileExists       = xva.CodeFileExists
	CodeFileIO           = xva.CodeFileIO
	CodeExportIncomplete = xva.CodeExportIncomplete
	CodeTimeout          = xva.CodeTimeout
	CodeCanceled         = xva.CodeCanceled
	CodeInterrupted      = "INTERRUPTED"
)

// ReqError 는 요청 단계 오류다. 처리기가 HTTP 상태로 바꾼다.
type ReqError struct {
	Code  string
	Field string
	Msg   string
}

func (e *ReqError) Error() string { return e.Msg }

func missing(field string) *ReqError {
	return &ReqError{Code: CodeMissingField, Field: field, Msg: "missing required field: " + field}
}

func invalid(field, msg string) *ReqError {
	return &ReqError{Code: CodeInvalidField, Field: field, Msg: msg}
}

// JobError 는 작업 단계 오류다. Detail 은 원문이다.
type JobError struct {
	Code   string
	Detail string
}

func (e *JobError) Error() string { return e.Code + ": " + e.Detail }

func jobErr(code string, err error) *JobError {
	return &JobError{Code: code, Detail: errText(err)}
}

// errText 는 XAPI 오류면 그 문장(예: "XAPI Error: UUID_INVALID | VM | …"), 아니면 오류 전체다.
func errText(err error) string {
	var xe *xapi.XAPIError
	if errors.As(err, &xe) {
		return xe.Error()
	}
	return err.Error()
}

// classify 는 오류를 작업 단계 코드로 바꾼다.
func classify(err error) *JobError {
	var je *JobError
	if errors.As(err, &je) {
		return je
	}
	var ve *xva.Error
	if errors.As(err, &ve) {
		return &JobError{Code: ve.Code, Detail: errText(ve.Err)}
	}
	var xe *xapi.XAPIError
	if errors.As(err, &xe) {
		return &JobError{Code: CodeXAPI, Detail: xe.Error()}
	}
	return &JobError{Code: CodeXAPI, Detail: err.Error()}
}

// loginError 는 로그인 실패를 인증 실패·접속 실패로 나눈다.
func loginError(err error) *JobError {
	var xe *xapi.XAPIError
	if errors.As(err, &xe) {
		if xe.Code == "SESSION_AUTHENTICATION_FAILED" {
			return &JobError{Code: CodeXAPIAuth, Detail: xe.Error()}
		}
		return &JobError{Code: CodeXAPI, Detail: xe.Error()}
	}
	return &JobError{Code: CodeXAPIConnect, Detail: err.Error()}
}
