# 구현 작업 순서

M0~M5, M6a 전부 구현 및 완료됨(go test -race 통과, 실제 qwen3:8b 재확인 포함). 각 단계의 완료 기준을 통과한 뒤 다음 단계로 이동한다. 남은 것은 M6b(docs/SDD.md §14.6~14.7, 설계만)다.

## M0. 계약 확정

- [x] go.mod와 목표 구조 생성 (이 저장소에는 기존 starter 코드가 없어 이전 작업 없음)
- [x] Config·Action·Observation·Hypothesis·Decision·Evidence·Verification 타입
- [x] JSON Schema, enum, 오류 코드, ID 참조 검증
- [x] Action.typed_args의 source_observation_id 출처 검증 (모델이 만든 URL·문서 ID 거부)
- [x] Planner/Judge/LLM/Tool/Store 인터페이스

완료 기준: 네트워크 없는 fixture 입출력을 파싱하고 잘못된 참조를 거부한다.

## M1. 로컬 실습 환경

- [x] Go 실습 앱: 로그인, 신원, 문서 목록, 문서 조회
- [x] 계정 A/B와 소유권·합성 marker seed
- [x] 취약/수정 모드와 평가 정답 격리
- [x] ARM64 Docker 빌드와 internal lab 네트워크
- [x] Ollama 호스트 연결은 inference 전용 경로로 검증 (2026-11-01, OrbStack: agent 컨테이너에서 `host.docker.internal:11434`로 호스트 Ollama 연결 확인, LAN 노출·Ollama 바인딩 변경 불필요. `labapp`은 내부 `lab` 네트워크로 `labapp:8080` 서비스명 연결. `deploy/compose.yaml`의 `lab` 네트워크 서브넷을 172.28.0.0/16으로 고정해 `configs/lab.example.json`의 `allowed_lab_cidrs`와 맞춤 — 전엔 호스트마다 랜덤 서브넷이라 안 맞았음)

완료 기준: 수동 요청으로 취약 모드 교차 접근과 수정 모드 차단을 각각 확인한다. → M0~M5 평가(README)로 이미 양쪽 모드를 확인했고, 위 항목으로 Docker 경로까지 확인했으므로 M1 전체 완료.

## M2. 실행 경계와 증거

- [x] 계정별 session, HTTP transport, origin/path/CIDR 검사
- [x] 리다이렉트 금지, proxy 비활성화, 시간·크기 제한
- [x] 예산·취소, 마스킹, 증거 해시와 append 기록
- [x] Dialer 단계에서 DNS 해석·CIDR 검사·연결을 한 번에 수행 (rebinding 방지)
- [x] 재로그인은 요청 예산에 포함, 계정당 1회 제한
- [x] content-type별 결정론적 본문 추출기와 1KiB 관찰 요약

완료 기준: AC-03~07, AC-10~11을 모델 없이 검증한다.

## M3. 결정론적 에이전트 루프

- [x] fake Planner/Judge와 상태 머신
- [x] 후보 필터 → 판단 → 최종 정책 → 실행 흐름
- [x] 반복 방지와 schema 복구 한도
- [x] Verifier의 계정·소유권·marker·재조회 검증 (평가기 정답 미사용, B 정상 응답의 관찰값과 비교)
- [x] 재로그인 실패 시 AC-03 경로 검증

완료 기준: AC-01~02, AC-08~09를 fixture로 통과한다.

## M4. Ollama와 판단 방식 연결

- [x] 정확한 모델/버전 선택(qwen3:8b, ResolveModel이 name@digest를 run.model_id에 기록), API 요청·응답 계약 확인(internal/llm/ollama)
- [x] 하나의 모델로 Planner/Judge 순차 호출 (internal/planner/ollama.go, internal/judge/ollama.go; JSON Schema로 구조화 출력, model.DecodeStrict로 재검증)
- [x] 컨텍스트 요약과 모델 호출 예산 (최근 12개 Observation만 전달, Budgets.MaxModelCalls/MaxCycles/MaxHTTPRequests를 매 호출 prompt에 명시)
- [x] 응답 실패·시간 초과·잘못된 JSON 처리 (agent.llmCtx가 LLMTimeoutSec로 호출별 데드라인 적용, MaxRecoveries=1 재시도 후 StopModelError)

완료 기준: 실제 모델이 관찰에 따라 다음 행동을 바꾸고, 모든 결정이 기록된다.

## M5. 보고서와 반복 평가

- [x] JSON/Markdown 보고서, 근거 부족·한계 명시 (internal/report; cmd/gosumdochi의 run/report 하위 명령)
- [x] 저장 기록에서 네트워크 없이 재생성 (state.ReadSnapshot + report.Build; tests/integration/m5_test.go로 AC-12 확인)
- [x] 취약/수정 모드 각 5회 평가 (qwen3:8b, 2026-10-31, host 환경; README "실제 모델 평가 결과" 참고)
- [x] Mac peak 메모리·시간·호출 수 측정 (agent 프로세스 peak 7.3~10.4 MiB; 시간은 배터리 Sleep Service 영향 가능 — README에 명시)
- [x] README 실행 절차와 알려진 한계 작성

평가 결과(최종): **취약 모드 5/5 confirmed**(목표 4/5 이상 달성), **수정 모드 5/5 정확히 기각**(false confirmed 0회, AC-02 충족). 초기 평가는 취약/수정 모드 모두 0/5로 실패했으나, 평가 과정에서 버그 3개를 찾아 고쳐 위 결과에 도달했다: (1) judge의 `selected_action_id` 조건부 필수를 Ollama 구조화 출력이 지키지 않던 문제, (2) planner에게 보내는 스키마의 `candidates.maxItems`가 실제 예산과 안 맞던 문제, (3) planner가 Verifier의 판단 사유(`Verification.Reasons`)를 아예 못 보고 있던 문제 — 이걸 전달한 것이 확정에 이른 결정적 수정이었다. 상세 경과는 README "실제 모델 평가 결과"에 기록.

완료 기준: AC-01~12와 spec의 실험 목표를 평가하고 결과를 기록한다. 미달이면 완료로 표시하지 않는다. → 달성.

## M6a. 인가 강화 + 수동 점검 (구현됨, go test -race 통과)

docs/SDD.md §14.1~14.4 설계 그대로 구현. 기존 typed_args·Action/Policy 계약은 바꾸지 않았다(model.AccountNone만 추가 — Planner가 제안 불가, agent가 code로만 사용).

- [x] labapp: `Credential.Role`(admin/""), `GET /api/admin/users`, `GET /api/public/docs/{id}`(무인증), `X-Debug-Backend` 헤더(vulnerable 모드) (SDD §14.3)
- [x] `model.Finding.Severity` 필드, rule_id→severity 고정 매핑(`model.SeverityForRule`), report Markdown에 심각도 표시 (SDD §14.2)
- [x] Verifier 규칙 `passive-exposure-v1` 추가 (internal/verifier/passive.go)
- [x] Verifier 규칙 `vertical-privesc-v1` 추가 (internal/verifier/privesc.go; doc-cross-read-v1과 같은 대조 구조 재사용)
- [x] Verifier 규칙 `unauth-access-v1` + agent의 code-driven 무인증 재조회 단계 (internal/verifier/unauth.go, agent.go의 `maybeCheckUnauth`; httptool에 세션 없는 `anon` 클라이언트 추가)

구현 중 설계와 달라진 점: unauth-access-v1의 baseline/comparison은 URL이 아니라 **marker로 상관**시킨다 — 무인증 미러(`/api/public/docs/{id}`)는 인증 원본(`/api/docs/{id}`)과 의도적으로 다른 경로라, URL을 키로 쓰면 절대 매칭되지 않는다(처음 구현에서 실제로 걸렸던 버그).

완료 기준: 세 규칙 각각 독립 fixture(internal/verifier) 및 통합 테스트(tests/integration/m6a_test.go)로 confirmed/rejected 경로 확인함, fake Planner/Judge로. 실제 qwen3:8b로도 확인함(tests/integration/m6a_live_test.go, 2026-11-01).

실제 모델 평가 결과: 수정 모드는 3회 재현 모두 안전 — 네 가설(H-1, H-PRIVESC, H-UNAUTH, H-PASSIVE) 전부 false confirm 0건. 취약 모드는 3회 재현 모두 같은 패턴: `unauth-access-v1`(code-driven, 모델 스킬 불필요)과 `passive-exposure-v1`(디버그 헤더가 항상 응답에 있어 모델 스킬 불필요) 은 매번 confirmed. `doc-cross-read-v1`(H-1)과 `vertical-privesc-v1`은 3회 모두 inconclusive — qwen3:8b가 네 가설을 동시에 쫓는 이 멀티룰 목표 문구 하에서는 cycle budget(15) 내에 교차계정 문서 읽기나 admin API의 B계정 비교 조회까지 도달하지 못했다. M4/M5 단독 doc-cross-read-v1 평가(5/5 confirmed)와 달리, 여기서는 run이 `StopModelError`("planner output invalid after recovery")로 조기 종료되는 경우가 매번 있었다 — 원인은 인프라 오류가 아니라, 모델이 이미 2개 가설을 확인한 뒤 더 이상 생산적인 새 행동을 제안하지 못하고 invalid/repeated 후보만 반복 제출하면서 `MaxRecoveries=1` 소진으로 안전하게 멈추는 것으로 확인됨(cycle 5~9 사이, 3회 모두 재현). 안전성(수정 모드 false confirm 0건)은 핵심 속성이므로 충족; H-1/H-PRIVESC의 미확인은 모델 역량·탐색 순서의 한계로 기록하고 이후 프롬프트 개선 과제로 남긴다.

## M6b. 탐색·반사형 입력 (설계 방향만, 미구현)

docs/SDD.md §14.6~14.7. M6a 완료 후 시작. reflected-input-v1은 Action/Policy의 파라미터 값 검사 계약을 ADR(`docs/adr/0002-...`)로 먼저 결정한 뒤 구현.

- [ ] internal/crawl: 관찰에서 링크·form 수집, 중복 제거, Planner가 탐색 행동 제안
- [ ] reflected-input-v1: ADR 작성 → Action.typed_args에 `param_value`, Policy의 값 검사 → 규칙 구현

완료 기준: M6a와 동일한 방식(fake 먼저, 실제 모델로 재확인).
