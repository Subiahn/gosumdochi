# Go슴도치 SDD v0.1

Software Design Description · 구현 전 설계 · 2026-10-18

## 1. 목적과 범위

사용자가 지정한 로컬 실습 앱과 두 테스트 계정을 이용해, AI가 관찰에서 가설을 만들고 필요한 요청을 선택하며 접근 권한 문제를 비교 검증한다. 보고서는 각 결론에서 실행 증거까지 추적 가능해야 한다.

v0.1 범위: 단일 대상 origin, 계정 A/B, 읽기 전용 문서 API, HTTP 도구, 단일 에이전트, Ollama 어댑터, 파일 저장, JSON/Markdown 보고서.
범위 밖: 임의 셸, 외부 공격 도구, 운영망 탐색, 자격 증명 추측, 파괴적 변경, 지속성, 다중 에이전트, 브라우저 자동화, 자동 수정, 학습·파인튜닝.

## 2. 설계 전제

- 개발 장비는 M5 통합 메모리 16GB Mac이다. 실제 성능은 구현 후 측정한다.
- Ollama는 Mac 호스트에서 실행하고, agent와 lab은 Docker 컨테이너로 실행한다.
- 초기 모델은 설정으로 선택한다. 앞서 제안한 4B급 모델은 후보이며 버전·도구 호출 호환성은 구현 단계에서 검증한다.
- Planner와 Judge는 하나의 로드된 모델을 순차 호출한다. 동일 모델 간 판단은 독립 검증으로 간주하지 않는다.
- Jev 제품·API의 구체적 계약은 제공되지 않았다. 본 설계의 Judge는 사용자가 설명한 타입 지정 판단 방식이며 Jev의 구현·속도·비용 특성을 주장하지 않는다.

## 3. 배포와 연결 경계

| 구성 | 위치 | 연결 |
| --- | --- | --- |
| Ollama | Mac 호스트 | agent의 추론 전용 클라이언트가 연결 |
| agent | Docker | control + lab 네트워크 |
| labapp | Docker | lab 네트워크만; SQLite는 컨테이너 내부 |
| 평가기 | 테스트 프로세스 | 실습 모드·정답 읽기; agent와 입력 공유 금지 |

lab 네트워크는 internal로 정의한다. 앱 UI가 필요하면 호스트 127.0.0.1에만 선택적으로 포트를 공개한다. control은 호스트 추론 서버 연결을 위한 경로다. 이 구성은 agent의 외부 접근을 네트워크 수준에서 완전히 차단하지 않는다. v0.1은 신뢰된 실행 코드의 policy와 transport 검사로 요청 경로를 통제한다. 더 강한 egress 격리는 별도 프록시 또는 방화벽 설계가 필요하다.

모델 연결용 클라이언트와 대상 HTTP 클라이언트를 분리한다. HTTP 도구에서 host.docker.internal, Ollama, 호스트 서비스에 접근할 수 없게 한다. 호스트 Ollama의 Docker 접근 설정은 실제 연결 테스트로 검증하며 LAN 공개는 피한다. Docker socket과 호스트 홈 디렉터리는 마운트하지 않는다.

## 4. 핵심 실행 흐름

1. Config 검증 → Run 생성 → 대상 및 예산 고정.
2. 설정의 비밀 참조를 해석하고 테스트 계정 세션을 준비한다. 실제 비밀은 모델 입력에 포함하지 않는다.
3. Planner에 목표, 축약된 관찰, 가설, 남은 예산, 도구 스키마를 전달한다.
4. Planner는 최대 3개 행동 후보와 가설 변경 제안을 출력한다.
5. 출력 스키마·참조 ID를 검증하고 명백한 범위 위반 후보를 제외한다.
6. Judge가 유효 후보 중 실행·추가 관찰·종료를 선택한다.
7. Policy가 선택된 실제 인자와 연결 대상을 다시 검사한다.
8. Executor가 하나의 도구를 실행하고 결과·증거·예산 소비를 저장한다.
9. Verifier가 대조 실험 조건을 검사한다. 미충족이면 confirmed로 올리지 않는다.
10. 다음 단계로 돌아가거나 종료하고 보고서를 만든다.

사이트 응답은 신뢰할 수 없는 관찰이다. 시스템 지시·도구 정의·범위 설정으로 승격하지 않는다. 모델이 응답 안의 지시를 따라도 스키마, 정책, 도구 권한은 변하지 않는다.

## 5. 구성 요소의 책임

| 요소 | 책임 | 갖지 않는 권한 |
| --- | --- | --- |
| Planner | 근거 기반 가설과 행동 후보 생성 | 도구 직접 실행 |
| Judge | 고정 enum 판단, 후보 ID 선택 | 범위 변경, 요청 직접 생성 |
| Policy | scope·method·path·예산 검사 | 취약 여부 추론 |
| Executor | 검사된 도구 실행, 취소·시간 제한 | 임의 셸 실행 |
| State | 사실·가설·판단의 별도 저장 | 모델 추측을 사실로 자동 승격 |
| Verifier | 계정별 대조 증거와 판정 조건 확인 | 상태 코드만으로 권한 문제 확정 |
| Reporter | 저장된 상태와 증거로 보고 | 새로운 발견사항 생성 |

## 6. 데이터 계약

모든 저장 레코드는 schema_version=1, run_id, UTC timestamp를 가진다. 아래 표는 필수 의미 계약이며 실제 Go 타입과 JSON Schema는 구현 단계에서 작성한다.

| 타입 | 주요 필드 |
| --- | --- |
| Run | id, goal, config_digest, model_id, model_options, status, stop_reason, budgets |
| Action | id, tool, typed_args, hypothesis_id, rationale_summary. typed_args의 URL·문서 ID는 기존 Observation이 노출한 값(source_observation_id로 참조)만 허용하며, 모델이 만든 ID는 Policy 이전 단계에서 거부한다 |
| Observation | id, action_id, result_status, evidence_ids, summary |
| Hypothesis | id, claim, supporting_evidence_ids, contradicting_evidence_ids, status |
| Decision | id, selected_action_id, decision, reason_code, evidence_ids |
| Evidence | id, action_id, account_ref, sanitized_request, sanitized_response, captured_body_hash, truncated |
| Verification | hypothesis_id, baseline_ids, comparison_ids, outcome, rule_id |
| Finding | id, hypothesis_id, title, impact, verification_id, evidence_ids, limitations |

Hypothesis.status: proposed / testing / confirmed / rejected / inconclusive.
Action result_status: completed / failed / denied / canceled.
Run.status: pending / running / completed / incomplete / canceled / failed.
Run.stop_reason: goal_verified / exploration_exhausted / budget_exhausted / repeated_action / model_error / tool_error / user_canceled / infrastructure_error.
completed는 계획한 범위의 실행 완료를 뜻하며 대상 전체의 안전을 보장하지 않는다. 탐색을 마쳐도 미해결 가설은 inconclusive로 유지한다.

### Judge 출력 예

```json
{
  "decision": "execute",
  "selected_action_id": "A-007",
  "reason_code": "missing_control_comparison",
  "evidence_ids": ["E-012", "E-015"]
}
```

decision enum: execute / stop / inconclusive. reason_code는 버전별 enum으로 정의한다. Judge의 inconclusive는 탐색을 더 진행하지 않겠다는 선택이며, 개별 Hypothesis.status는 Verifier 규칙과 상태 저장소만 변경한다. 존재하지 않는 후보·증거 ID는 거부한다. stop은 Verifier의 확정 상태나 탐색 종료 상태와 함께 검증한다. Judge가 confirmed를 직접 부여하지 못한다.
신뢰도 점수는 v0.1에서 생략한다. 추후 추가해도 검증 데이터로 보정하기 전에는 확률로 표시하지 않는다.

## 7. 도구 및 인증 계약

초기 도구는 http_request 하나다. 입력은 method, URL, account_ref, 제한된 headers이며 결과는 status, headers, 제한된 text body, duration_ms, error_code다.
대상 데이터 요청은 GET만 허용한다. 로그인 POST는 세션 구성 요소가 설정에 명시된 로그인 경로에만 수행한다. 모델이 로그인 경로·비밀·인증 헤더를 바꿀 수 없다.
계정별 cookie jar 및 token 저장소를 분리한다. account_ref=A/B만 모델에 전달한다. 권한 경계 비교 시 한 계정의 쿠키가 다른 계정 요청에 섞이지 않아야 한다.

HTTP 도구는 http/https만 허용하고 userinfo를 거부한다. origin(스킴·호스트·포트)과 허용 경로를 검사한다. 자동 리다이렉트는 기본 비활성화한다. 향후 허용할 경우 매 홉마다 scope와 주소를 다시 검사한다.
DNS 해석 결과는 설정한 lab CIDR 안에 있어야 하며 검사한 IP에 연결한다. 구현은 Dialer 단계에서 해석·검사·연결을 한 번에 수행해 검사와 연결 사이의 재해석(DNS rebinding)을 막는다. 기본 시스템 proxy 환경변수는 사용하지 않는다. 루프백·링크로컬·호스트 관리 주소는 실습 앱용 주소로 명시한 경우 외에는 거부한다. TLS 검증을 기본 활성화하며 테스트 인증서는 전용 CA로 신뢰한다.

## 8. 실행 예산과 실패 처리

초기값은 측정 전 제안이며 설정으로 변경할 수 있다.

| 항목 | 초기값 |
| --- | --- |
| agent decision cycle | 최대 15회 |
| HTTP 요청 | 로그인 포함 최대 30회; 재시도도 계산 |
| 모델 호출 | 최대 32회; 출력 복구 포함 |
| 후보 수 | cycle당 최대 3개 |
| HTTP timeout | 요청당 10초 |
| LLM timeout | 호출당 120초 |
| 전체 실행 | 15분 |
| 응답 본문 | 압축 해제 후 최대 64KiB, 초과 시 truncated |
| 동시 실행 | 도구 1개, 모델 호출 1개 |
| 컨텍스트 | 8K 후보; 토큰 예산은 adapter에서 확인 |

세션 만료로 인한 재로그인은 HTTP 요청 예산 30회에 포함하며, 계정당 재로그인은 1회까지 허용한다. 실패하면 해당 계정 비교를 확정하지 않고 AC-03 경로로 처리한다.

모델 컨텍스트에는 원문 본문을 넣지 않는다. 본문은 content-type별 결정론적 추출기(JSON은 허용 필드 화이트리스트, HTML은 제목·링크·폼 요약)를 거쳐 관찰 요약(기본 최대 1KiB)과 evidence_id로만 전달한다. Verifier는 모델 입력과 무관하게 디스크의 마스킹 증거 원본을 사용한다. 추출 실패 시 해당 관찰은 inconclusive 근거로만 쓴다.

동일 method·정규화 URL·account_ref·body 조합의 재실행은 검증 목적 1회까지 허용하고 이후 반복이면 종료 또는 다른 후보를 선택한다.
모델 출력이 잘못되면 구문 오류만 알려 한 번 복구를 시도한다. 반복 실패 시 fail closed하며 불완전 보고서를 저장한다. 도구 오류는 관찰에 기록하고 자동 취약점으로 해석하지 않는다. Ctrl+C는 진행 중 HTTP/LLM 요청을 취소하고 저장 가능한 기록을 flush한다.
전체 프롬프트에 모든 본문을 누적하지 않는다. 최근 관찰과 가설 요약, 증거 ID를 전달하고 지정 예산을 초과하면 오래된 관찰을 요약한다. 원본에 가까운 마스킹 증거는 디스크에 남긴다.

## 9. 증거와 상태 저장

초기는 DB 의존성을 줄이기 위해 실행별 파일 저장을 사용한다.

```text
runs/<run-id>/
├── run.json
├── events.jsonl
├── state.json
├── evidence/E-0001.json
├── report.json
└── report.md
```

events.jsonl은 단일 writer가 순서대로 기록한다. state.json과 보고서는 임시 파일 후 rename으로 교체한다. 프로세스 중단 후 기록을 읽어 불완전 보고서를 재생성할 수 있어야 한다. 실행 재개는 v0.1 범위 밖이다.
Authorization, Cookie, Set-Cookie, 비밀번호, 알려진 token과 설정된 민감 필드를 저장·모델 전달 전에 마스킹한다. 임의 비밀을 완벽히 탐지한다고 보장하지 않는다. 초기 데이터는 합성 데이터만 사용한다.
해시는 실제로 저장한 마스킹·절단된 body bytes의 SHA-256이다. 원래 전체 응답의 해시처럼 표현하지 않는다. truncated 플래그와 원래 Content-Length(있다면)를 함께 기록한다. 증거 해시는 변조 방지 서명이나 독립 진위 보증이 아니다.

## 10. 접근 권한 검증 규칙

실습 앱은 계정별 문서와 고유한 합성 marker를 제공한다. 평가기만 취약/수정 모드를 알고 agent에는 모드를 주지 않는다.

confirmed를 위해 다음이 모두 필요하다.
1. A와 B의 신원 확인 요청이 각각 성공하고 서로 다른 계정임이 확인된다.
2. B가 자기 문서를 조회한 정상 기준 응답이 있다.
3. A가 동일 문서를 조회한 비교 응답이 있다.
4. 비교 응답에 B 소유 자원임을 확인할 소유자 필드와 문서 marker가 포함된다.
5. A의 정상 자기 문서 응답을 포함해 세션 혼동을 배제한다.
6. 캐시 영향을 줄이는 요청과 독립 재조회 1회로 재현한다.

Verifier는 에이전트가 실행 중 수집한 Evidence만 입력으로 쓴다. 평가기의 정답·seed·모드 정보와 대상 문서 ID·소유자·marker의 기대값은 사용하지 않으며, 소유자와 marker는 B의 정상 기준 응답에서 관찰한 값과 비교한다.

200만 같거나 에러 페이지 body가 같다는 이유로 확정하지 않는다. 403/404와 본문 부재가 관찰되면 해당 가설을 기각할 수 있으나 앱 전체가 안전하다고 결론 내리지 않는다. 소유권·인증 증거가 부족하면 inconclusive다. 규칙은 이 실습 시나리오 전용이며 범용 권한 검증기로 주장하지 않는다.

## 11. 설정 계약과 CLI

계획된 설정: target_origin, allowed_path_prefixes, allowed_lab_cidrs, accounts(A/B secret refs), llm(provider/model/base_url), budgets, output_dir.
비밀은 Git 제외 파일 또는 환경변수 참조로 주입한다. run.json에는 비밀 대신 참조명과 비민감 설정 digest만 저장한다.

계획된 명령:
- gosumdochi plan -config <path>: 설정 검증과 계획 출력, 네트워크 실행 없음.
- gosumdochi run -config <path>: 에이전트 실행.
- gosumdochi report -run <dir>: 저장된 기록에서 보고서 재생성.

종료 코드(CI 사용 시 0이 "취약점 없음"이 아님에 유의): 0=완료(발견 여부 무관), 1=설정/인프라 실패, 2=예산·모델 오류 등 불완전 실행, 130=사용자 취소. 발견 여부는 report의 findings로 판단한다.

## 12. 검증과 관측

모델 없이 fake Planner/Judge로 실행 상태, 예산, 취소, 오류 처리를 검증한다. 실제 로컬 모델 실험은 고정 seed 데이터·설정·모델 식별자·프롬프트 버전을 기록한다.
측정값: confirmed 정탐 수, fixed 모드 오탐 수, inconclusive 수, 도구/모델 호출 수, 실행 시간, peak 메모리, 판단 스키마 실패 수. 작은 실험의 점수를 일반적인 공격 성능으로 확대 해석하지 않는다.

핵심 수용 조건: 취약 모드에서 증거 연결된 확인 결과, 수정 모드에서 false confirmed 없음, 범위 밖 요청 네트워크 전송 없음, 계정 세션 분리, 모델 오류 시 잘못된 성공 표시 없음.

## 13. 미결정과 확장

구현 전에 결정할 항목: 실제 모델 태그/버전, Ollama API 호환 방식, Docker에서 호스트 추론 서버 접속 설정, macOS 자원 할당값. 실제 Jev 연동은 API·배포 방식·라이선스 확인 이후다.
후속 확장: SQLite state store, 브라우저 도구, 다른 검증 시나리오, 추론 adapter 교체, Judge 독립 모델 비교. 각 확장은 기존 수용 테스트를 유지한 상태로 추가한다.
