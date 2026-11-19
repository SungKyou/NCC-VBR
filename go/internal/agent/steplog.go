package agent

import (
	"fmt"

	"ncc-vbr/internal/logx"
)

// backupLogFile 은 원본 BACKUP_LOG_FILE("./backup.log") 이다.
const backupLogFile = "backup.log"

// 단계 기호와 문구. 원본 BACKUP_LOG 의 switch 다.
var stepText = map[byte]string{
	'S': "SNAPSHOT create",
	'P': "SNAPSHOT VM property Change",
	'E': "VM BACKUP",
	'D': "SNAPSHOT destroy",
	'I': "VM IMPORT",
	'R': "Partial VM BACKUP",
}

// StepLog 는 원본 BACKUP_LOG / IMPORT_LOG 다(같은 파일 backup.log, 최대 크기를 넘으면 .old).
type StepLog struct {
	log *logx.Logger
}

// Backup 은 BACKUP_LOG(step, vm_uuid, file_name, fmt, ...) 다.
func (s *StepLog) Backup(step byte, vmUUID, fileName, format string, args ...any) {
	s.write(step, "VM-UUID", vmUUID, fileName, format, args...)
}

// Import 는 IMPORT_LOG('I', sr_uuid, file_name, fmt, ...) 다.
func (s *StepLog) Import(srUUID, fileName, format string, args ...any) {
	s.write('I', "SR-UUID", srUUID, fileName, format, args...)
}

func (s *StepLog) write(step byte, idName, id, fileName, format string, args ...any) {
	if s == nil || s.log == nil {
		return
	}
	text, ok := stepText[step]
	if !ok {
		s.log.Printf("BACKUP LOG parameter invalid.  Skip this request.[%c]\n", step)
		return
	}
	line := fmt.Sprintf("[%s]%s: %s=%s filename=%s\n\t%s\n",
		s.log.Now().Format("2006-01-02 15:04:05"), text, idName, id, fileName, fmt.Sprintf(format, args...))
	s.log.Append(backupLogFile, line, true, "Xenserver BACKUP Log file open error")
}
