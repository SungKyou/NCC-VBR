// 출처: ncc-xen backend/pkg/xapi/mixins/task.go (커밋 043c6d5, 2026-11-17 복사)
// 바뀐 부분: docs/agent/08-copied-code-register.md CP-04

package xapi

// task.go — XAPI task 레코드 범용 헬퍼 (2026-10-19 신규)
//
// VMClient(vm.go)에 TaskGetStatus/TaskGetProgress/TaskGetResult/TaskGetErrorInfo/
// TaskDestroy가 이미 있지만, task 자체는 VM-specific 개념이 아니라 Async.*로
// 시작한 모든 작업(VM/Host/SR 등)이 공유하는 XAPI 객체다. HostClient.EvacuateAsync/
// SRClient의 Async 변환들이 VMClient를 빌려 쓰지 않도록 동일한 로직을 여기
// 자유 함수로 분리했다. 기존 VMClient 메서드는 동작 동일한 코드를 괜히 건드려
// 회귀 위험을 만들지 않기 위해 그대로 두고 손대지 않았다(내용 중복 허용).
import (
	"context"
	"fmt"
	"strings"
)

// TaskGetStatus는 Task의 현재 상태를 반환합니다.
// 반환값: "pending" | "success" | "failure" | "cancelling" | "cancelled"
func TaskGetStatus(ctx context.Context, c *Client, taskRef string) (string, error) {
	val, err := c.Call(ctx, "task.get_status", c.SessionRef(), taskRef)
	if err != nil {
		return "", fmt.Errorf("task.get_status failed: %w", err)
	}
	return val.Str(), nil
}

// TaskGetProgress는 Task의 현재 진행률을 반환합니다 (0.0 ~ 1.0).
func TaskGetProgress(ctx context.Context, c *Client, taskRef string) (float64, error) {
	val, err := c.Call(ctx, "task.get_progress", c.SessionRef(), taskRef)
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

// TaskGetResult는 완료된 Task의 결과값을 반환합니다.
// status가 "success"일 때만 의미 있는 값을 반환합니다. 결과가 OpaqueRef가
// 아닌 Async 작업(예: host.evacuate는 결과값이 없음)은 빈 문자열이 반환될 수
// 있습니다 — 호출자가 필요 여부를 판단하세요.
func TaskGetResult(ctx context.Context, c *Client, taskRef string) (string, error) {
	val, err := c.Call(ctx, "task.get_result", c.SessionRef(), taskRef)
	if err != nil {
		return "", fmt.Errorf("task.get_result failed: %w", err)
	}
	raw := val.Str()
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "<value>")
	raw = strings.TrimSuffix(raw, "</value>")
	raw = strings.TrimPrefix(raw, "<ref>")
	raw = strings.TrimSuffix(raw, "</ref>")
	return strings.TrimSpace(raw), nil
}

// TaskGetErrorInfo는 실패한 Task의 오류 정보를 반환합니다.
func TaskGetErrorInfo(ctx context.Context, c *Client, taskRef string) (string, error) {
	val, err := c.Call(ctx, "task.get_error_info", c.SessionRef(), taskRef)
	if err != nil {
		return "", fmt.Errorf("task.get_error_info failed: %w", err)
	}
	slice := val.ToStringSlice()
	if len(slice) == 0 {
		return "알 수 없는 오류", nil
	}
	return strings.Join(slice, ": "), nil
}

// TaskDestroy는 Task 레코드를 정리합니다. Async 작업 완료/실패 후 반드시
// defer로 호출해야 합니다.
func TaskDestroy(ctx context.Context, c *Client, taskRef string) error {
	_, err := c.Call(ctx, "task.destroy", c.SessionRef(), taskRef)
	if err != nil {
		return fmt.Errorf("task.destroy failed: %w", err)
	}
	return nil
}
