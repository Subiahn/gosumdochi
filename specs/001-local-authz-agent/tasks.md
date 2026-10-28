# 구현 작업 순서

M0·M2·M3은 구현됨(go test -race 통과). M1은 Docker→호스트 Ollama 경로 확인만 남았다. M4는 Ollama 어댑터만 구현되었고 Planner/Judge 프롬프트 연결은 미구현이다. M5는 미구현이다. 각 단계의 완료 기준을 통과한 뒤 다음 단계로 이동한다.

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

- [x] 정확한 모델/버전 선택(qwen3:8b, digest는 ResolveModel로 run에 기록 예정), API 요청·응답 계약 확인(internal/llm/ollama)
- [ ] 하나의 모델로 Planner/Judge 순차 호출
- [ ] 컨텍스트 요약과 모델 호출 예산
- [ ] 응답 실패·시간 초과·잘못된 JSON 처리

완료 기준: 실제 모델이 관찰에 따라 다음 행동을 바꾸고, 모든 결정이 기록된다.

## M5. 보고서와 반복 평가

- [ ] JSON/Markdown 보고서, 근거 부족·한계 명시
- [ ] 저장 기록에서 네트워크 없이 재생성
- [ ] 취약/수정 모드 각 5회 평가
- [ ] Mac peak 메모리·시간·호출 수 측정
- [ ] README 실행 절차와 알려진 한계 작성

완료 기준: AC-01~12와 spec의 실험 목표를 평가하고 결과를 기록한다. 미달이면 완료로 표시하지 않는다.
