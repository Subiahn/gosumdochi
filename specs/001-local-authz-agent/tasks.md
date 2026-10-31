# 구현 작업 순서

M0~M5는 구현됨(go test -race 통과). M1은 Docker→호스트 Ollama 경로 확인만 남았다. 각 단계의 완료 기준을 통과한 뒤 다음 단계로 이동한다.

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
- [ ] Ollama 호스트 연결은 inference 전용 경로로 검증 (경로는 대체 서버로 확인함; 호스트에서 qwen3:8b 어댑터 호출은 확인함; Docker agent 컨테이너에서 host.docker.internal 경로는 미확인)

완료 기준: 수동 요청으로 취약 모드 교차 접근과 수정 모드 차단을 각각 확인한다.

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

## M6. 다중 규칙·탐색 (설계만, 미구현)

docs/SDD.md §14에 설계를 적어 두었다. 코드는 아직 없다. M5 완료 후 순서대로 진행한다. 대상은 계속 사용자 소유 labapp이며 범위 밖 항목(SDD §1)은 유지한다.

- [ ] labapp에 관리자 역할·전용 엔드포인트, 무인증 엔드포인트, 디버그 헤더 분기, 반사 파라미터 추가 (SDD §14.4)
- [ ] Verifier 규칙 `unauth-access-v1` 추가
- [ ] Verifier 규칙 `vertical-privesc-v1` 추가
- [ ] Verifier 규칙 `passive-exposure-v1` 추가 (Finding에 severity 필드 추가 포함)
- [ ] internal/crawl: 관찰에서 링크·form 수집, 중복 제거, Planner가 탐색 행동 제안
- [ ] reflected-input-v1: Action/Policy의 파라미터 값 검사 계약을 ADR로 먼저 결정한 뒤 구현

완료 기준: 새 규칙마다 fake Planner/Judge로 독립 fixture/통합 테스트 통과, 그 다음 실제 모델로 재확인.
