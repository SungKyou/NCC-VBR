package xapi

// restore_ops.go 는 vbr_vm 복구 후처리에 쓰는 XAPI 호출이다.
// copied from ncc-xen backend/pkg/xapi/mixins/vm.go SetNameLabelByRef, SetAffinity @043c6d5 (CP-VM-12), 2026-11-19.
// 바뀐 부분(docs/vm/08-copied-code-register.md CP-VM-12):
//   - 오류 문장을 i18n.RawT 대신 고정 영어 문장으로 썼다(이 패키지의 다른 함수와 같은 방식).
//   - SetAffinity 는 VM uuid 대신 이미 가진 VM 참조를 받는다(가져온 직후라 참조가 있다).

import (
	"context"
	"fmt"
)

// SetNameLabelByRef 는 VM.set_name_label 이다.
func (v *VMClient) SetNameLabelByRef(ctx context.Context, vmRef, name string) error {
	if _, err := v.c.Call(ctx, "VM.set_name_label", v.c.SessionRef(), vmRef, name); err != nil {
		return fmt.Errorf("VM.set_name_label failed: %w", err)
	}
	return nil
}

// SetAffinityByRef 는 VM 의 Home Server 를 정한다. hostUUID 가 비면 지정을 지운다(OpaqueRef:NULL).
func (v *VMClient) SetAffinityByRef(ctx context.Context, vmRef, hostUUID string) error {
	hostRef := NullRef
	if hostUUID != "" {
		val, err := v.c.Call(ctx, "host.get_by_uuid", v.c.SessionRef(), hostUUID)
		if err != nil {
			return fmt.Errorf("host.get_by_uuid failed (uuid=%s): %w", hostUUID, err)
		}
		hostRef = val.Str()
	}
	if _, err := v.c.Call(ctx, "VM.set_affinity", v.c.SessionRef(), vmRef, hostRef); err != nil {
		return fmt.Errorf("VM.set_affinity failed: %w", err)
	}
	return nil
}
