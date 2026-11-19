package xapi

// backup_ops.go 는 ncc-vbr에서 새로 작성한 XAPI 호출이다(ncc-xen에서 복사하지 않았다).
// 원본 vbr_agent가 xe 명령으로 하던 일 가운데 ncc-xen에 없는 것을 담는다.
//
//	원본 xe 명령                                        이 파일
//	xe template-param-set is-a-template=false ...      SetIsATemplate(vm.go) + SetHAAlwaysRun
//	xe vbd-list vm-uuid=... type=Disk                   DiskVBDs
//	xe vbd-param-set uuid=... other-config:owner=false  SetVBDOwnerFalse
//	xe vdi-destroy uuid=...                             DestroyVDI
//	xe vm-uninstall uuid=... force=true                 UninstallSnapshot
//	(vm-export 프로세스 kill)                            CancelTask

import (
	"context"
	"fmt"
	"strconv"
)

// NullRef 는 XAPI의 빈 참조다.
const NullRef = "OpaqueRef:NULL"

// DiskVBD 는 VM에 붙은 디스크 하나다.
type DiskVBD struct {
	Ref        string
	UUID       string
	UserDevice int
	VDIRef     string
}

// SetHAAlwaysRun 은 VM.set_ha_always_run 이다.
// XenServer 7 이후 이 필드는 폐기 예정(deprecated)이라 호출이 실패할 수 있다.
// 호출하는 쪽에서 실패를 무시할지 정한다.
func (v *VMClient) SetHAAlwaysRun(ctx context.Context, vmRef string, value bool) error {
	_, err := v.c.Call(ctx, "VM.set_ha_always_run", v.c.SessionRef(), vmRef, value)
	if err != nil {
		return fmt.Errorf("VM.set_ha_always_run failed: %w", err)
	}
	return nil
}

// DiskVBDs 는 VM의 VBD 가운데 type이 Disk인 것을 돌려준다(원본 xe vbd-list ... type=Disk).
func (v *VMClient) DiskVBDs(ctx context.Context, vmRef string) ([]DiskVBD, error) {
	refsVal, err := v.c.Call(ctx, "VM.get_VBDs", v.c.SessionRef(), vmRef)
	if err != nil {
		return nil, fmt.Errorf("VM.get_VBDs failed: %w", err)
	}
	var out []DiskVBD
	for _, ref := range refsVal.ToStringSlice() {
		recVal, err := v.c.Call(ctx, "VBD.get_record", v.c.SessionRef(), ref)
		if err != nil {
			return nil, fmt.Errorf("VBD.get_record failed: %w", err)
		}
		rec := recVal.ToMap()
		if str(rec["type"]) != "Disk" {
			continue
		}
		dev, err := strconv.Atoi(str(rec["userdevice"]))
		if err != nil {
			return nil, fmt.Errorf("VBD %s userdevice is not a number: %q", str(rec["uuid"]), str(rec["userdevice"]))
		}
		out = append(out, DiskVBD{Ref: ref, UUID: str(rec["uuid"]), UserDevice: dev, VDIRef: str(rec["VDI"])})
	}
	return out, nil
}

// SetVBDOwnerFalse 는 원본 xe vbd-param-set other-config:owner=false 와 같다.
func (v *VMClient) SetVBDOwnerFalse(ctx context.Context, vbdRef string) error {
	// add_to_other_config 는 이미 있는 키에 실패하므로 먼저 지운다. 없는 키를 지우는 것은 오류가 아니다.
	if _, err := v.c.Call(ctx, "VBD.remove_from_other_config", v.c.SessionRef(), vbdRef, "owner"); err != nil {
		return fmt.Errorf("VBD.remove_from_other_config failed: %w", err)
	}
	if _, err := v.c.Call(ctx, "VBD.add_to_other_config", v.c.SessionRef(), vbdRef, "owner", "false"); err != nil {
		return fmt.Errorf("VBD.add_to_other_config failed: %w", err)
	}
	return nil
}

// DestroyVDI 는 VDI.destroy 다.
func (v *VMClient) DestroyVDI(ctx context.Context, vdiRef string) error {
	if _, err := v.c.Call(ctx, "VDI.destroy", v.c.SessionRef(), vdiRef); err != nil {
		return fmt.Errorf("VDI.destroy failed: %w", err)
	}
	return nil
}

// UninstallSnapshot 은 원본 xe vm-uninstall uuid=<스냅샷> force=true 를 대신한다.
// 스냅샷 VM의 디스크 가운데 스냅샷 VDI(is_a_snapshot=true)만 지운 뒤 VM 레코드를 지운다.
// 스냅샷이 아닌 VDI는 다른 VM과 함께 쓰는 디스크일 수 있어 지우지 않는다.
// ncc-xen의 임시 스냅샷 정리(VM.destroy만)는 디스크를 남길 수 있어 이 함수를 따로 둔다.
func (v *VMClient) UninstallSnapshot(ctx context.Context, snapRef string) error {
	vbds, err := v.DiskVBDs(ctx, snapRef)
	if err != nil {
		return err
	}
	for _, d := range vbds {
		if d.VDIRef == "" || d.VDIRef == NullRef {
			continue
		}
		snapVal, err := v.c.Call(ctx, "VDI.get_is_a_snapshot", v.c.SessionRef(), d.VDIRef)
		if err != nil {
			// 부분 백업에서 이미 지운 VDI면 핸들이 없다. 다음 디스크로 넘어간다.
			if xe, ok := err.(*XAPIError); ok && xe.Code == "HANDLE_INVALID" {
				continue
			}
			return fmt.Errorf("VDI.get_is_a_snapshot failed: %w", err)
		}
		if snapVal.Str() != "1" && snapVal.Str() != "true" {
			continue
		}
		if err := v.DestroyVDI(ctx, d.VDIRef); err != nil {
			return err
		}
	}
	return v.DestroyByRef(ctx, snapRef)
}

// CancelTask 는 task.cancel 이다.
func (v *VMClient) CancelTask(ctx context.Context, taskRef string) error {
	if _, err := v.c.Call(ctx, "task.cancel", v.c.SessionRef(), taskRef); err != nil {
		return fmt.Errorf("task.cancel failed: %w", err)
	}
	return nil
}

// SRByUUID 는 SR.get_by_uuid 다.
func (v *VMClient) SRByUUID(ctx context.Context, uuid string) (string, error) {
	val, err := v.c.Call(ctx, "SR.get_by_uuid", v.c.SessionRef(), uuid)
	if err != nil {
		return "", fmt.Errorf("SR.get_by_uuid failed (uuid=%s): %w", uuid, err)
	}
	return val.Str(), nil
}

func str(v interface{}) string {
	s, _ := v.(string)
	return s
}
