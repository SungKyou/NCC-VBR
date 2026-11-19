package job

// task_durability.go — 파일 기반 "접수된 작업(accepted task)" 멱등성/durability
// + 결과 전달 outbox (2026-09-15).
//
// ── 배경 ────────────────────────────────────────────────────────
//   handler.go는 HTTP 202를 먼저 반환한 뒤 goroutine만 띄우고 어디에도
//   기록하지 않았다. taskId 중복 검사도 없었다. 결과 콜백(SendSuccess/
//   SendFailure)도 단발 POST라 실패하면 그걸로 끝이었다(query_helper.go의
//   공용 sendSuccess/sendFailure가 err만 로그로 남기고 재시도하지 않음).
//   그래서 (1) 202 이후 crash 시 작업 자체가 유실되고, (2) 동일 taskId
//   재전송 시 destroy/move 같은 작업이 중복 실행될 수 있고, (3) 실제 XAPI
//   작업은 성공했는데 결과 POST만 실패하면 Middleware가 영구 미확정 상태로
//   남는 문제가 있었다.
//
// ── 설계 ────────────────────────────────────────────────────────
//   Redis 대신 로컬 파일(JSON Lines)을 씁니다 — 이 메커니즘 자체가
//   "장애 대응용"인데 Redis에 의존하면 Redis가 같이 죽었을 때 안전장치가
//   무력화됩니다. 또한 이 프로젝트의 방향성(air-gapped 환경 지원,
//   file-based workflow, 단일 바이너리 경량 배포)과도 맞습니다.
//
//   Xen Orchestra 사례를 참고해 "완전 자동 재개(resume)"는 구현하지
//   않습니다 — 성숙한 제품(XO)도 대부분 작업은 재시작 시 자동 재개 없이
//   "중단됨"으로 표시만 하고, 재시도는 운영자/Middleware 판단에 맡깁니다.
//   여기서도 재기동 시 미완료 task를 찾으면 "interrupted"로 표시하고
//   outbox를 통해 Middleware에 알리는 것까지만 합니다.
//
//   accepted.jsonl  — 접수된 task 기록(멱등성 검사용 + 재기동 복구용)
//   outbox_pending.jsonl   — 아직 전달 안 된 결과
//   outbox_delivered.jsonl — 전달 완료된 결과(감사/디버깅용 아카이브)
//   outbox_deadletter.jsonl — 재시도를 포기한 결과(운영자 확인용, 2026-09-21)
//
//   모든 파일은 JSON Lines(줄마다 JSON 객체 하나) append-only이며,
//   압축(compact)할 때는 임시 파일에 새로 쓰고 os.Rename으로 원자적
//   교체합니다 — 도중에 프로세스가 죽어도 파일이 깨지지 않습니다.
//
// ── outbox_delivered.jsonl 무한 증가 문제 수정 (2026-09-29) ─────────
//   accepted.jsonl / outbox_pending.jsonl은 각각 MarkDone/deliverOnce의
//   compact 경로로 자연히 비워지지만, outbox_delivered.jsonl은 append만
//   되고 정리하는 경로가 전혀 없어 재기동 없이도, 재기동을 거듭해도
//   무한정 커졌습니다. 실측(2026-09-29, 테스트 서버): 17,054줄 /
//   1,371,864,350 bytes(약 1.31GB). 이 파일의 유일한 소비자는
//   knownTaskIDs()이고, 이는 재기동 시 InitDurability()가 부팅 직후
//   1회만 파일 전체를 []map[string]interface{}로 파싱해 메모리에 올리는
//   용도인데, 실측 시 이 파싱 구간이 애플리케이션 로그가 20초 이상 완전히
//   멈추는 시간과 정확히 겹쳤고, free -m 상 메모리 사용량 최고점(avail
//   1.4GB까지 하락)과도 일치함을 확인했습니다.
//
//   해결책은 resetDeliveredIfDrained()입니다 — outbox_pending.jsonl이
//   완전히 비어 있을 때(=현재 전송 대기 중인 미송신 데이터가 하나도 없을
//   때)만 outbox_delivered.jsonl을 0바이트로 truncate합니다. 미송신
//   데이터가 하나라도 남아있는 동안에는 절대 건드리지 않습니다(사용자
//   요청, 2026-09-29) — delivered.jsonl을 섣불리 비우면 knownTaskIDs()가
//   "이미 전달됨"으로 인식하지 못해 재기동 복구 로직이 정상적으로 배달
//   대기 중인 결과를 오인할 위험이 있기 때문입니다. deliverOnce()가 호출될
//   때마다(5초 tick 또는 Enqueue 직후 wake) 큐가 비어 있는지 확인하고,
//   비어 있으면 정리합니다 — 재기동 시점뿐 아니라 장시간 재기동 없이
//   운영되는 경우에도 파일이 무한정 쌓이지 않도록 합니다.
//   outbox_deadletter.jsonl은 운영자가 실패 건을 직접 확인해야 하는
//   용도이므로 2026-09-29 시점에는 자동 정리 대상에 포함하지 않았습니다
//   (당시 273KB/5줄로 메모리 영향도 없음) — 아래 2026-09-30 항목에서
//   보관 기간(retention) 방식으로 정리 정책이 추가되었습니다.
//
// ── Dead Letter 처리 보완 (2026-09-30) ──────────────────────────────
//   (1) Dead Letter 이동 시 accepted.jsonl 정리 누락 수정
//       기존 deliverOnce()의 Dead Letter 분기는 onDelivered(MarkDone)를
//       호출하지 않아, 결과가 outbox_deadletter.jsonl로 옮겨진 task가
//       accepted.jsonl에 done=false로 영구히 남았습니다. 실측(2026-09-30,
//       테스트 서버): Middleware가 모르는 taskId(수동 테스트 요청)의 결과가
//       거부(재시도 불가)되어 deadletter로 이동했는데, accepted.jsonl에
//       같은 taskId 1건(POOL_QUERY/get_all)이 그대로 남아 있었습니다. 이
//       상태에서 재기동하면 InitDurability()가 이 task를 "중단됨"으로
//       오인해 거짓 failure를 Middleware에 다시 전송하고, 그 결과가 또
//       Dead Letter로 쌓였습니다.
//       → moveToDeadLetter()가 deadletter.jsonl 기록에 성공한 경우에만
//         onDelivered(MarkDone)를 호출합니다. 기록에 실패하면 결과를 어디에도
//         남기지 못한 것이므로 accepted.jsonl 항목을 그대로 두어, 재기동 시
//         최소한 interrupted 알림이 나가도록 합니다(기존 안전망 유지).
//       → knownTaskIDs()가 deadletter.jsonl도 확인합니다. 이미 Dead Letter로
//         결과가 확보된 task는 재기동 시 거짓 interrupted 알림 없이
//         accepted.jsonl만 조용히 정리됩니다(이 수정 이전에 남은 기존
//         accepted 항목도 첫 재기동에서 정리됨).
//       → knownTaskIDs()는 파일 전체를 map으로 파싱하지 않고 줄 단위로 읽으며
//         taskId 필드만 디코딩합니다(jsonlCollectTaskIDs) — 2026-09-29에
//         확인한 "대용량 jsonl 전체 파싱 → 부팅 메모리 급증" 유형의 재발을
//         막기 위함입니다.
//   (2) Dead Letter 보관 기간(retention) 정리 — B안
//       outbox_deadletter.jsonl은 자동 삭제 경로가 없어 무한정 쌓일 수
//       있었습니다. 운영자가 원인을 확인할 시간은 보장하면서 증가는 막기
//       위해, Dead Letter로 이동한 시각(deadLetteredAt, 이전 레코드는
//       createdAt로 대체)이 보관 기간(sysconfig.yaml task_store.
//       deadletter_retention_days, 기본 30일)을 넘은 줄만 제거합니다.
//       기동 시 1회 + 이후 1시간 간격으로 실행하며, 제거할 줄이 없으면 파일을
//       다시 쓰지 않습니다. 날짜를 해석할 수 없는 줄은 삭제하지 않고 보존합니다.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ncc/stdlogger/logger"
	"github.com/ncc/xen-backend/internal/gateway"
	"github.com/ncc/xen-backend/internal/i18n"
)

// ─────────────────────────────────────────────
// jsonl 파일 helper (append / compact / loadAll 공용)
// ─────────────────────────────────────────────

func jsonlAppend(mu *sync.Mutex, path string, record interface{}) error {
	mu.Lock()
	defer mu.Unlock()

	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// jsonlLoadAll은 path의 모든 줄을 raw JSON(map)으로 읽어 반환합니다.
// 파일이 없으면 빈 슬라이스를 반환합니다(에러 아님). 마지막 줄이 깨져
// 있어도(프로세스가 쓰는 도중 죽은 경우) 그 줄만 건너뛰고 계속 읽습니다.
func jsonlLoadAll(path string) ([]map[string]interface{}, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []map[string]interface{}
	// 2026-09-18 긴급 수정: 기존 bufio.Scanner(1줄 최대 1MB)는 한도를 넘는
	// 줄을 만나면 에러를 반환하지 않고 조용히 스캔을 멈췄다(sc.Err() 미확인).
	// VM_QUERY/DEVICE_QUERY get_all, INFRA_SYNC device 결과처럼 1MB를 넘는
	// 레코드가 pending 선두에 오면 워커가 "레코드 0건"으로 판단해 아무것도
	// 보내지 않고 로그도 남기지 않는 완전 정지 상태가 됐다(실기 재현).
	// 더 위험하게는 jsonlCompactTo가 같은 함수로 읽어 다시 쓰므로, 앞 항목이
	// 하나라도 전달되는 순간 그 줄과 뒤 레코드가 파일에서 통째로 사라졌다.
	// bufio.Reader.ReadBytes로 줄 길이 제한 없이 읽고, 읽기 에러는 반환한다.
	rd := bufio.NewReaderSize(f, 64*1024)
	for {
		line, readErr := rd.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			if len(line) > 0 {
				var m map[string]interface{}
				if err := json.Unmarshal(line, &m); err == nil {
					out = append(out, m)
				}
				// 파싱 실패 줄은 건너뜀 (크래시 도중 쓰던 줄일 수 있음)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return out, readErr
		}
	}
	return out, nil
}

// jsonlCollectTaskIDs는 path의 각 줄에서 taskId 필드만 디코딩해 out에 추가합니다
// (2026-09-30). jsonlLoadAll과 달리 파일 전체를 map으로 메모리에 올리지 않고
// 한 줄씩만 읽으므로, 크기가 큰 jsonl(과거 delivered 1.31GB 사례)에서도 메모리
// 사용량이 가장 긴 줄 하나 수준으로 제한됩니다. 파일이 없거나 읽기 에러가
// 나면 그 시점까지 모은 결과만 반영하고 조용히 반환합니다(knownTaskIDs의 기존
// 동작 — 에러 무시 — 과 동일).
func jsonlCollectTaskIDs(path string, out map[string]bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	rd := bufio.NewReaderSize(f, 64*1024)
	for {
		line, readErr := rd.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			var head struct {
				TaskID string `json:"taskId"`
			}
			if err := json.Unmarshal(line, &head); err == nil && head.TaskID != "" {
				out[head.TaskID] = true
			}
		}
		if readErr != nil {
			return
		}
	}
}

// jsonlCompactTo는 srcPath의 각 줄을 keep에 넣고, keep이 true를 반환하는
// 줄만 새 파일에 써서 원자적으로 srcPath를 교체합니다.
func jsonlCompactTo(mu *sync.Mutex, srcPath string, keep func(map[string]interface{}) bool) error {
	mu.Lock()
	defer mu.Unlock()

	records, err := jsonlLoadAll(srcPath)
	if err != nil {
		return err
	}
	tmpPath := srcPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	for _, r := range records {
		if !keep(r) {
			continue
		}
		line, err := json.Marshal(r)
		if err != nil {
			continue
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, srcPath)
}

// ─────────────────────────────────────────────
// AcceptedTaskStore — taskId 멱등성 + 접수 기록 durability
// ─────────────────────────────────────────────

type acceptedRecord struct {
	TaskID     string `json:"taskId"`
	JobType    string `json:"jobType"`
	Action     string `json:"action"`
	TargetType string `json:"targetType"`
	TargetUUID string `json:"targetUUID"`
	AcceptedAt string `json:"acceptedAt"`
	Done       bool   `json:"done"`
}

// AcceptedTaskStore는 accepted.jsonl을 관리합니다.
type AcceptedTaskStore struct {
	mu     sync.Mutex
	path   string
	seen   map[string]bool // taskId → true (완료 여부 무관, 한 번이라도 접수됐으면)
	seenMu sync.RWMutex
}

func newAcceptedTaskStore(dir string) *AcceptedTaskStore {
	return &AcceptedTaskStore{
		path: filepath.Join(dir, "accepted.jsonl"),
		seen: make(map[string]bool),
	}
}

// IsDuplicate는 이 taskId가 이미 접수된 적 있는지 반환합니다.
func (s *AcceptedTaskStore) IsDuplicate(taskID string) bool {
	s.seenMu.RLock()
	defer s.seenMu.RUnlock()
	return s.seen[taskID]
}

// Accept는 taskId를 접수 기록에 추가합니다(파일 append + 메모리 인덱스).
func (s *AcceptedTaskStore) Accept(taskID, jobType, action, targetType, targetUUID string) error {
	s.seenMu.Lock()
	s.seen[taskID] = true
	s.seenMu.Unlock()

	return jsonlAppend(&s.mu, s.path, acceptedRecord{
		TaskID:     taskID,
		JobType:    jobType,
		Action:     action,
		TargetType: targetType,
		TargetUUID: targetUUID,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// TryAccept는 중복 확인과 접수 기록을 하나의 락 안에서 처리합니다(🆕 2026-10-29).
// IsDuplicate → Accept를 따로 호출하면 같은 taskId가 거의 동시에 두 번 들어올 때
// 둘 다 IsDuplicate를 통과해 중복 실행될 수 있었습니다. 이미 접수된 taskId면
// false를 반환하고 아무것도 기록하지 않습니다.
func (s *AcceptedTaskStore) TryAccept(taskID, jobType, action, targetType, targetUUID string) (accepted bool, err error) {
	s.seenMu.Lock()
	if s.seen[taskID] {
		s.seenMu.Unlock()
		return false, nil
	}
	s.seen[taskID] = true
	s.seenMu.Unlock()

	return true, jsonlAppend(&s.mu, s.path, acceptedRecord{
		TaskID:     taskID,
		JobType:    jobType,
		Action:     action,
		TargetType: targetType,
		TargetUUID: targetUUID,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// MarkDone은 outbox가 이 taskId의 결과를 성공적으로 전달했을 때(또는 Dead
// Letter로 결과를 보존했을 때 — 2026-09-30) 호출됩니다.
// 다음 압축(compact) 시 accepted.jsonl에서 제거됩니다. 메모리 인덱스에서는
// 유지합니다(같은 프로세스 수명 동안은 재전송돼도 계속 중복으로 판단 —
// 압축 후에도 재시작 전까지는 멱등성이 깨지지 않도록).
func (s *AcceptedTaskStore) MarkDone(taskID string) {
	_ = jsonlCompactTo(&s.mu, s.path, func(r map[string]interface{}) bool {
		if id, _ := r["taskId"].(string); id == taskID {
			return false // 제거
		}
		return true
	})
}

// LoadAndRecover는 재기동 시 accepted.jsonl을 읽어 메모리 인덱스를 채우고,
// done 표시 없이 남아있는(=완료 확인이 안 된) 항목 목록을 반환합니다.
// 자동으로 재실행하지 않습니다 — 호출자가 "interrupted" 상태로 Middleware에
// 알리는 용도로만 사용합니다(Xen Orchestra의 접근 방식 참고).
func (s *AcceptedTaskStore) LoadAndRecover() []acceptedRecord {
	records, err := jsonlLoadAll(s.path)
	if err != nil {
		return nil
	}
	s.seenMu.Lock()
	defer s.seenMu.Unlock()

	var interrupted []acceptedRecord
	for _, r := range records {
		taskID, _ := r["taskId"].(string)
		if taskID == "" {
			continue
		}
		s.seen[taskID] = true
		interrupted = append(interrupted, acceptedRecord{
			TaskID:     taskID,
			JobType:    strVal(r, "jobType"),
			Action:     strVal(r, "action"),
			TargetType: strVal(r, "targetType"),
			TargetUUID: strVal(r, "targetUUID"),
			AcceptedAt: strVal(r, "acceptedAt"),
		})
	}
	return interrupted
}

func strVal(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// ─────────────────────────────────────────────
// Outbox — 결과(성공/실패) 전달 durability
// ─────────────────────────────────────────────

type outboxRecord struct {
	TaskID     string                 `json:"taskId"`
	JobType    string                 `json:"jobType"`
	Action     string                 `json:"action"`
	TargetType string                 `json:"targetType"`
	TargetUUID string                 `json:"targetUUID"`
	Message    string                 `json:"message"`
	Options    map[string]interface{} `json:"options,omitempty"`
	Kind       string                 `json:"kind"` // "success" | "failure"
	CreatedAt  string                 `json:"createdAt"`
	// LastError는 Dead Letter로 옮겨질 때만 채워집니다(운영자가 원인을
	// 바로 확인할 수 있도록 — 성공/재시도 중인 레코드에는 없음). 2026-09-21
	// (B-13) — event_durability.go의 eventOutboxRecord.LastError와 동일 패턴.
	LastError string `json:"lastError,omitempty"`
	// DeadLetteredAt은 Dead Letter로 옮겨진 시각(RFC3339, UTC)입니다
	// (2026-09-30). 보관 기간(retention) 계산의 기준이며, 이 필드가 없는 이전
	// 레코드는 CreatedAt을 대신 사용합니다.
	DeadLetteredAt string `json:"deadLetteredAt,omitempty"`
}

// defaultTaskMaxAge는 재시도 가능한 에러(5xx/429/408 등)라도 이 기간이
// 지나면 Dead Letter로 옮깁니다. main.go에서 sysconfig.yaml의
// task_store.max_age_hours로 덮어쓸 수 있습니다(2026-09-21, B-13).
const defaultTaskMaxAge = 24 * time.Hour

// defaultDeadLetterRetention은 outbox_deadletter.jsonl에 레코드를 보관하는
// 기본 기간입니다(2026-09-30). main.go에서 sysconfig.yaml의
// task_store.deadletter_retention_days로 덮어쓸 수 있습니다.
const defaultDeadLetterRetention = 30 * 24 * time.Hour

// taskDeadLetterPruneInterval은 Run 루프에서 Dead Letter 보관 기간 정리를
// 다시 시도하는 최소 간격입니다(2026-09-30).
const taskDeadLetterPruneInterval = time.Hour

// Outbox는 outbox_pending.jsonl / outbox_delivered.jsonl / outbox_deadletter.jsonl을
// 관리하고, 백그라운드 워커로 실제 전달(gw.SendSuccess/SendFailure)을 재시도합니다.
type Outbox struct {
	mu             sync.Mutex
	pendingPath    string
	deliverPath    string
	deadLetterPath string
	gw             *gateway.Client
	log            *logger.Entry
	onDelivered    func(taskID string) // 전달 성공/Dead Letter 보존 시 콜백(accepted.jsonl 정리용)
	maxAge         time.Duration
	wake           chan struct{}

	// retention: Dead Letter 레코드 보관 기간(2026-09-30). 0 이하면 정리하지
	// 않습니다. newOutbox가 defaultDeadLetterRetention으로 초기화하고,
	// InitDurability가 설정값으로 덮어씁니다.
	retention time.Duration
	// lastPrune: 마지막으로 pruneDeadLetter를 시도한 시각. InitDurability(Run 시작
	// 전)와 Run 고루틴에서만 순차적으로 접근합니다.
	lastPrune time.Time

	// wasFailing: 직전 배달 시도가 실패였는지(2026-09-15, 로그 폭주 방지용).
	// true인 동안은 gateway.WithQuietLog로 전송 전문/에러 로그를 생략하고,
	// 다시 성공하는 순간(복구)에만 알림 로그를 남깁니다. Middleware가 오래
	// 다운돼 있을 때 5초마다 거대한 요청 전문이 통째로 반복 출력되던 문제.
	wasFailing bool
}

func newOutbox(dir string, gw *gateway.Client, log *logger.Entry, maxAge time.Duration, onDelivered func(string)) *Outbox {
	if maxAge <= 0 {
		maxAge = defaultTaskMaxAge
	}
	return &Outbox{
		pendingPath:    filepath.Join(dir, "outbox_pending.jsonl"),
		deliverPath:    filepath.Join(dir, "outbox_delivered.jsonl"),
		deadLetterPath: filepath.Join(dir, "outbox_deadletter.jsonl"),
		gw:             gw,
		log:            log,
		maxAge:         maxAge,
		onDelivered:    onDelivered,
		wake:           make(chan struct{}, 1),
		retention:      defaultDeadLetterRetention,
	}
}

// logInfo / logError는 o.log가 nil이어도 안전한 로그 helper입니다(2026-09-30).
// 로거를 초기화하지 않는 단위 테스트에서 resetDeliveredIfDrained /
// moveToDeadLetter / pruneDeadLetter를 직접 호출할 수 있게 하기 위함이며,
// i18n.T는 o.log가 nil이 아닐 때만 평가합니다.
func (o *Outbox) logInfo(key string, kv ...interface{}) {
	if o.log == nil {
		return
	}
	o.log.Info(i18n.T(key), kv...)
}

func (o *Outbox) logError(key string, kv ...interface{}) {
	if o.log == nil {
		return
	}
	o.log.Error(i18n.T(key), kv...)
}

// Enqueue는 결과를 pending 파일에 기록합니다. 파일 write는 동기적이며,
// 실제 네트워크 전달은 별도 백그라운드 워커가 담당합니다(이 함수는
// 디스크 쓰기가 끝나면 바로 반환 — Middleware 응답을 기다리지 않음).
func (o *Outbox) Enqueue(r outboxRecord) error {
	r.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := jsonlAppend(&o.mu, o.pendingPath, r); err != nil {
		return err
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
	return nil
}

// Run은 백그라운드 배달 워커입니다. main.go에서 go outbox.Run(ctx)로 시작합니다.
func (o *Outbox) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		o.deliverOnce(ctx)
		// 2026-09-30: Dead Letter 보관 기간 정리 — 마지막 시도 후 1시간 이상
		// 지났을 때만 실행합니다(기동 시 첫 정리는 InitDurability가 수행).
		if time.Since(o.lastPrune) >= taskDeadLetterPruneInterval {
			o.pruneDeadLetter()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-o.wake:
		}
	}
}

// recordAge는 RFC3339 형식의 createdAt으로부터 경과 시간을 계산합니다.
// 파싱에 실패하면(빈 문자열 등) 0을 반환합니다 — 이 경우 shouldDeadLetter의
// maxAge 판정에서는 "아직 안 지남"으로 취급됩니다(안전한 쪽으로 기본값).
// pkg/sync/event_durability.go의 동일 이름 함수와 로직은 같지만, 패키지가
// 달라(job vs sync) 별도 구현입니다(session discipline — 검증된 기존 코드를
// 건드리지 않기 위해 공유 추출은 이번 범위에서 하지 않음).
func recordAge(createdAt string) time.Duration {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return 0
	}
	return time.Since(t)
}

// shouldDeadLetter는 실패한 결과를 재시도 큐에 계속 둘지, Dead Letter로
// 옮길지 판정하는 순수 함수입니다(네트워크·로거·파일 I/O 없음 — 단위
// 테스트로 직접 검증 가능). 판정 기준(event_durability.go의 EventOutbox와
// 동일 기준, 2026-09-21 B-13):
//  1. sendErr가 재시도 불가능한 에러(GatewayError.Retryable == false,
//     예: 400/401)면 무조건 Dead Letter
//  2. 재시도 가능한 에러라도 최초 발생(createdAt)으로부터 maxAge 경과 시
//     Dead Letter
func shouldDeadLetter(sendErr error, createdAt string, maxAge time.Duration) bool {
	if !gateway.IsRetryable(sendErr) {
		return true
	}
	return recordAge(createdAt) > maxAge
}

// outboxDeadLetterExpired는 Dead Letter 레코드가 보관 기간을 넘겼는지 판정하는
// 순수 함수입니다(2026-09-30). 기준 시각은 deadLetteredAt이며, 없으면(이 필드
// 도입 이전 레코드) createdAt을 사용합니다. retention이 0 이하이거나 기준
// 시각을 해석할 수 없으면 false — 즉 삭제하지 않고 보존합니다.
func outboxDeadLetterExpired(r map[string]interface{}, retention time.Duration) bool {
	if retention <= 0 {
		return false
	}
	ts := strVal(r, "deadLetteredAt")
	if ts == "" {
		ts = strVal(r, "createdAt")
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	return time.Since(t) > retention
}

// moveToDeadLetter는 재시도를 포기한 결과를 outbox_deadletter.jsonl에 기록합니다
// (2026-09-30, deliverOnce의 Dead Letter 분기를 테스트 가능하도록 분리).
//
// deadletter.jsonl 기록에 성공하면 onDelivered(=accepted.jsonl의 MarkDone)를
// 호출합니다 — 결과가 Dead Letter에 보존되었으므로 이 task를 "중단됨"으로 다시
// 알릴 필요가 없습니다. 기록에 실패하면 결과가 어디에도 남지 않은 것이므로
// onDelivered를 호출하지 않아 accepted.jsonl 항목을 유지합니다(재기동 시
// interrupted 알림이 나가는 기존 안전망).
func (o *Outbox) moveToDeadLetter(r outboxRecord, sendErr error) {
	age := recordAge(r.CreatedAt)
	r.LastError = sendErr.Error()
	r.DeadLetteredAt = time.Now().UTC().Format(time.RFC3339)
	o.logError("task_durability.l010",
		"taskId", r.TaskID, "jobType", r.JobType, "action", r.Action,
		"error", sendErr, "age", age.String())
	if err := jsonlAppend(&o.mu, o.deadLetterPath, r); err != nil {
		o.logError("task_durability.l011", "taskId", r.TaskID, "error", err)
		return
	}
	if o.onDelivered != nil {
		o.onDelivered(r.TaskID)
		o.logInfo("task_durability.l014", "taskId", r.TaskID)
	}
}

// pruneDeadLetter는 보관 기간(o.retention)을 넘긴 Dead Letter 레코드만 제거합니다
// (2026-09-30, B안). 제거할 레코드가 없으면 파일을 다시 쓰지 않습니다.
// 기준 시각을 해석할 수 없는 줄은 삭제하지 않습니다(outboxDeadLetterExpired).
func (o *Outbox) pruneDeadLetter() {
	o.lastPrune = time.Now()
	if o.retention <= 0 {
		return
	}

	o.mu.Lock()
	info, statErr := os.Stat(o.deadLetterPath)
	o.mu.Unlock()
	if statErr != nil || info.Size() == 0 {
		return // 파일이 없거나 비어 있음
	}

	o.mu.Lock()
	records, err := jsonlLoadAll(o.deadLetterPath)
	o.mu.Unlock()
	if err != nil {
		o.logError("task_durability.l016", "path", o.deadLetterPath, "error", err)
		return
	}
	expired := 0
	for _, r := range records {
		if outboxDeadLetterExpired(r, o.retention) {
			expired++
		}
	}
	if expired == 0 {
		return
	}

	removed := 0
	if err := jsonlCompactTo(&o.mu, o.deadLetterPath, func(r map[string]interface{}) bool {
		if outboxDeadLetterExpired(r, o.retention) {
			removed++
			return false
		}
		return true
	}); err != nil {
		o.logError("task_durability.l016", "path", o.deadLetterPath, "error", err)
		return
	}
	o.logInfo("task_durability.l015", "removed", removed, "retention", o.retention.String())
}

// deliverOnce는 pending 파일을 처음부터 순서대로 전달 시도합니다.
//
// 2026-09-21 (B-13 수정) — 기존에는 어떤 실패든(재시도 불가능한 400/401
// 포함) 무조건 그 지점에서 break하여 뒤 taskId들을 영구 대기시켰습니다.
// 이제 event_durability.go의 EventOutbox와 동일한 패턴으로 바뀝니다:
// 각 레코드는 셋 중 하나로 끝납니다 — 전달 성공(delivered.jsonl로),
// 영구 실패 판정(deadletter.jsonl로 — Dead Letter는 뒤 레코드를 막지 않고
// 계속 진행), 또는 아직 재시도 가능(그 지점에서 멈추고 다음 tick에 처음부터
// 재시도 — 같은 taskId의 상태가 순서 뒤바뀌어 전달되는 것을 막기 위해 여기서만
// 순서를 보장합니다). taskId별 순서 보장이 목적이므로(§B-13 분석 — cross-taskId
// 스킵 허용 여부는 별도 결정사항 D-02, 이번 범위 아님), 성공/데드레터로 이미
// 처리된 항목이 뒤 taskId를 막지 않는 것이 핵심입니다.
func (o *Outbox) deliverOnce(ctx context.Context) {
	// o.mu로 보호해서 읽습니다 — 락 없이 읽으면 이 시점 직후 Enqueue()가
	// 새 항목을 추가했을 때, 아래에서 "다 성공했으니 파일을 비운다"고
	// 판단하면서 그 사이 추가된 항목까지 같이 날아가는 경쟁 상태가
	// 있었습니다(2026-09-15 — "3건 중 1건 누락" 재현 원인).
	o.mu.Lock()
	records, err := jsonlLoadAll(o.pendingPath)
	o.mu.Unlock()
	if err != nil || len(records) == 0 {
		// 2026-09-29: 보낼 게 없는 상태(pending 비어있음)입니다 — 이 기회에
		// outbox_delivered.jsonl 정리 대상인지 함께 확인합니다. 매 tick(5초)
		// 또는 Enqueue 직후 wake마다 호출되지만, resetDeliveredIfDrained()는
		// 이미 비어 있으면 os.Stat만 확인하고 바로 반환하므로 정상 운영 중
		// 반복 호출 비용은 무시할 수 있는 수준입니다.
		o.resetDeliveredIfDrained()
		return
	}

	finishedThrough := -1 // 이 인덱스까지는 성공 또는 dead-letter로 "처리 완료"

	for i, raw := range records {
		var r outboxRecord
		b, _ := json.Marshal(raw)
		if err := json.Unmarshal(b, &r); err != nil {
			finishedThrough = i // 파싱 안 되는 줄은 버림(더 붙잡고 있어도 의미 없음)
			continue
		}

		// quiet 여부는 레코드마다 "현재" o.wasFailing 기준으로 재계산합니다.
		// 배치 안에 여러 건이 있을 때 앞선 레코드에서 복구(wasFailing
		// true→false)되면, 뒤 레코드는 더 이상 quiet가 아니어야 합니다.
		// 루프 진입 전에 한 번만 계산하면 복구 이후 레코드도 계속 quiet로
		// 처리돼 "실제로는 보냈는데 로그가 안 남는" 문제가 있었습니다
		// (2026-09-15).
		sendCtx := ctx
		if o.wasFailing {
			sendCtx = gateway.WithQuietLog(ctx)
		}

		var sendErr error
		if r.Kind == "failure" {
			sendErr = o.gw.SendFailure(sendCtx, r.TaskID, r.JobType, r.Action, r.TargetType, r.TargetUUID, r.Message)
		} else {
			sendErr = o.gw.SendSuccess(sendCtx, r.TaskID, r.JobType, r.Action, r.TargetType, r.TargetUUID, r.Message, r.Options)
		}

		if sendErr == nil {
			if o.wasFailing {
				// 실패하다가 이번에 처음 성공 — 복구 알림 (전문 재출력은 안 함)
				o.log.Info(i18n.T("task_durability.l006"), "taskId", r.TaskID)
				o.wasFailing = false
			}
			if err := jsonlAppend(&o.mu, o.deliverPath, r); err != nil {
				// 조용히 무시하지 않고 반드시 로그 — 실제 전달은 됐는데 기록만
				// 실패하면 이후 재기동 복구 로직(knownTaskIDs)이 이 taskId를
				// 못 찾아 잘못된 interrupted 알림을 보낼 수 있습니다.
				o.log.Error(i18n.T("task_durability.l007"), "taskId", r.TaskID, "error", err)
			}
			if o.onDelivered != nil {
				o.onDelivered(r.TaskID)
			}
			finishedThrough = i
			continue
		}

		// 실패 — Dead Letter 판정 (2026-09-21, B-13)
		if shouldDeadLetter(sendErr, r.CreatedAt, o.maxAge) {
			// 2026-09-30: 기록 + accepted.jsonl 정리를 moveToDeadLetter로 분리
			o.moveToDeadLetter(r, sendErr)
			finishedThrough = i
			continue
		}

		// 재시도 가능 + 아직 maxAge 안 지남 → 여기서 멈추고 다음 tick에 처음부터
		if !o.wasFailing {
			// 이 실패 스트릭의 첫 실패만 로그 — 이후 반복 재시도는 조용히.
			o.log.Warn(i18n.T("task_durability.l001"), "taskId", r.TaskID, "error", sendErr)
		}
		o.wasFailing = true
		break // 순서 보장을 위해 여기서 중단, 다음 tick에 처음부터 재시도
	}

	if finishedThrough < 0 {
		return
	}
	// 앞에서부터 finishedThrough(포함)까지 제거하고 나머지는 남깁니다(전부
	// 성공/데드레터 처리된 경우도 동일한 경로 사용 — jsonlCompactTo는 실행
	// 시점에 파일을 다시 읽으므로, 그 사이 Enqueue()가 추가한 새 항목이
	// 있어도 안전하게 보존됩니다).
	keepFrom := finishedThrough + 1
	idx := -1
	if err := jsonlCompactTo(&o.mu, o.pendingPath, func(map[string]interface{}) bool {
		idx++
		return idx >= keepFrom
	}); err != nil {
		o.log.Error(i18n.T("task_durability.l008"), "error", err)
	}

	// 2026-09-29: 이번 라운드로 pending이 완전히 비워졌을 수 있으니 다시
	// 확인합니다(compact 직후 pending을 다시 읽는 비용은 보통 pending이
	// 작으므로 무시할 수 있는 수준 — 대량 pending이 쌓인 비정상 상황이라면
	// 어차피 len(pending)>0이라 resetDeliveredIfDrained() 내부에서 즉시
	// 반환됩니다).
	o.resetDeliveredIfDrained()
}

// resetDeliveredIfDrained는 outbox_pending.jsonl이 완전히 비어 있을 때만
// (=현재 미송신 상태로 남아있는 결과가 하나도 없을 때만) outbox_delivered.jsonl을
// 0바이트로 truncate합니다.
//
// outbox_delivered.jsonl의 유일한 소비자는 knownTaskIDs()이며, 이는 재기동
// 시 "완료 확인이 안 된(accepted.jsonl에 남은) task가 사실은 이미 전달됐거나
// 전달 대기 중인지" 판단하는 용도입니다. pending이 완전히 비어있는 시점
// 이전에 접수된 모든 task는 이미 성공적으로 전달되어 onDelivered 콜백을 통해
// accepted.jsonl에서도 정리(MarkDone)되었으므로, 그 시점 이전의
// delivered.jsonl 기록은 더 이상 재기동 복구에 필요하지 않습니다.
//
// 반대로 pending에 미송신 항목이 하나라도 남아있으면 절대 정리하지
// 않습니다(2026-09-29, 사용자 요청) — 그 경우 delivered.jsonl을 섣불리
// 비우면 "이미 전달된 것으로 착각해 중복 전달을 막으려는" knownTaskIDs()의
// 판단 근거가 사라져, 재기동 복구 로직이 오작동할 위험이 있기 때문입니다.
//
// 이 함수는 deliverOnce()가 호출될 때마다(5초 tick 또는 Enqueue 직후 wake)
// 함께 호출되므로, 프로세스를 재기동하지 않고 장시간 운영하는 경우에도
// delivered.jsonl이 무한정 쌓이지 않습니다. 이미 비어있으면 os.Stat만
// 확인하고 즉시 반환하므로 정상 운영 중 반복 호출 비용은 무시할 수 있는
// 수준입니다.
func (o *Outbox) resetDeliveredIfDrained() {
	o.mu.Lock()
	defer o.mu.Unlock()

	pending, err := jsonlLoadAll(o.pendingPath)
	if err != nil || len(pending) > 0 {
		return // 미송신 데이터가 남아있음 — 정리하지 않음
	}

	info, err := os.Stat(o.deliverPath)
	if err != nil || info.Size() == 0 {
		return // 파일이 없거나 이미 비어있음 — 할 일 없음
	}

	freedBytes := info.Size()
	if err := os.Truncate(o.deliverPath, 0); err != nil {
		o.logError("task_durability.l012", "path", o.deliverPath, "error", err)
		return
	}
	o.logInfo("task_durability.l013", "bytes_freed", freedBytes)
}

// ─────────────────────────────────────────────
// 패키지 레벨 싱글턴 — main.go에서 InitDurability로 1회 초기화
// ─────────────────────────────────────────────

var (
	taskStore *AcceptedTaskStore
	resultBox *Outbox
)

// knownTaskIDs는 pending/delivered/deadletter 파일에 이미 결과가 존재하는(전달
// 완료됐거나 전달 대기 중이거나 Dead Letter로 보존된) taskId 집합을 반환합니다.
// 재기동 복구 시 "완료 미확인"으로 보이는 accepted.jsonl 항목이 사실은 이미
// 처리·전달된 것인지 판단하는 데 씁니다(2026-09-15 — delivered.jsonl 기록과
// accepted.jsonl 정리(MarkDone) 사이의 비원자적 구간에서 크래시가 나면 이미
// 전달된 결과도 "interrupted"로 잘못 재알림되는 문제가 있었음).
//
// 2026-09-30: (1) deadletter.jsonl 추가 — Dead Letter로 보존된 task를 "중단됨"으로
// 오인하지 않도록. (2) 파일 전체를 map으로 파싱하지 않고 줄 단위로 taskId만
// 읽도록 변경(jsonlCollectTaskIDs) — 대용량 파일에서의 부팅 메모리 급증 방지.
func (o *Outbox) knownTaskIDs() map[string]bool {
	out := make(map[string]bool)
	for _, path := range []string{o.pendingPath, o.deliverPath, o.deadLetterPath} {
		jsonlCollectTaskIDs(path, out)
	}
	return out
}

// InitDurability는 accepted-task 저장소와 outbox를 초기화하고, 재기동 시
// 미완료로 남은 task를 찾아 "interrupted"로 outbox에 알린 뒤, outbox 배달
// 워커를 시작합니다. main()에서 앱 시작 시 정확히 1회 호출해야 합니다.
//
// maxAge: 재시도 가능한 에러라도 이 기간이 지나면 Dead Letter로 옮깁니다.
// 0 이하를 넘기면 defaultTaskMaxAge(24시간)를 사용합니다(2026-09-21, B-13).
//
// deadLetterRetention: Dead Letter 레코드 보관 기간입니다. 0 이하를 넘기면
// defaultDeadLetterRetention(30일)을 사용합니다(2026-09-30). 시그니처 변경:
// 이 파라미터가 maxAge 뒤, log 앞에 추가되었습니다(main.go 호출부 수정 필요).
func InitDurability(ctx context.Context, dir string, gw *gateway.Client, maxAge, deadLetterRetention time.Duration, log *logger.Entry) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	taskStore = newAcceptedTaskStore(dir)
	resultBox = newOutbox(dir, gw, log, maxAge, func(taskID string) {
		taskStore.MarkDone(taskID)
	})
	if deadLetterRetention > 0 {
		resultBox.retention = deadLetterRetention
	}

	// 2026-09-30: 부팅 시 보관 기간이 지난 Dead Letter를 먼저 정리합니다(이후
	// 정리는 Run 루프가 1시간 간격으로 수행).
	resultBox.pruneDeadLetter()

	interrupted := taskStore.LoadAndRecover()
	knownResults := resultBox.knownTaskIDs()

	for _, r := range interrupted {
		if knownResults[r.TaskID] {
			// 이미 결과가 확보돼 있음(전달 완료, 전달 대기 중, 또는 Dead Letter
			// 보존) — 가짜 interrupted 알림을 보내지 않고 accepted.jsonl만
			// 정리합니다. (원래 delivered 직후 실행됐어야 할 MarkDone이 크래시로
			// 인해 못 끝났던 경우를 여기서 뒤늦게 마무리)
			log.Info(i18n.T("task_durability.l003"), "taskId", r.TaskID)
			taskStore.MarkDone(r.TaskID)
			continue
		}
		log.Warn(i18n.T("task_durability.l002"), "taskId", r.TaskID, "jobType", r.JobType, "action", r.Action)
		_ = resultBox.Enqueue(outboxRecord{
			TaskID:     r.TaskID,
			JobType:    r.JobType,
			Action:     r.Action,
			TargetType: r.TargetType,
			TargetUUID: r.TargetUUID,
			Message:    "이전 실행이 완료 확인 없이 중단되었습니다(프로세스 재시작). 실제 처리 여부를 XenServer에서 직접 확인해 주세요.",
			Kind:       "failure",
		})
	}

	go resultBox.Run(ctx)
	return nil
}

// isDuplicateTask / acceptTask / enqueueResult는 handler.go / query_helper.go
// 에서 사용하는 패키지 레벨 접근 함수입니다. InitDurability가 호출되지
// 않았으면(taskStore/resultBox가 nil이면) 게이팅 없이 통과시킵니다 —
// 테스트 등에서 초기화를 생략해도 기존 동작이 깨지지 않도록.

func isDuplicateTask(taskID string) bool {
	if taskStore == nil {
		return false
	}
	return taskStore.IsDuplicate(taskID)
}

func acceptTask(taskID, jobType, action, targetType, targetUUID string) {
	if taskStore == nil {
		return
	}
	_ = taskStore.Accept(taskID, jobType, action, targetType, targetUUID)
}

// tryAcceptTask는 TryAccept 래퍼입니다(🆕 2026-10-29). taskStore 미초기화 시
// 기존 acceptTask와 같이 항상 접수(true)로 처리합니다. 파일 기록 실패는
// 접수 자체를 막지 않고(메모리 인덱스로 중복 방지는 유지) 오류만 돌려줍니다.
func tryAcceptTask(taskID, jobType, action, targetType, targetUUID string) (bool, error) {
	if taskStore == nil {
		return true, nil
	}
	return taskStore.TryAccept(taskID, jobType, action, targetType, targetUUID)
}

func enqueueResult(taskID, jobType, action, targetType, targetUUID, message string, options map[string]interface{}, kind string) error {
	if resultBox == nil {
		return nil // outbox 미초기화 — 호출자가 fallback으로 직접 gw 호출해야 함
	}
	return resultBox.Enqueue(outboxRecord{
		TaskID: taskID, JobType: jobType, Action: action,
		TargetType: targetType, TargetUUID: targetUUID,
		Message: message, Options: options, Kind: kind,
	})
}
