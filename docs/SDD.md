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

모델 연결용 클라이언트와 대상 HTTP 클라이언트를 분리한다. HTTP 도구에서 host.docker.internal, Ollama, 호스트 서비스에 접근할 수 없게 한다(Policy가 target_origin·allowed_lab_cidrs로 강제). 호스트 Ollama의 Docker 접근은 실제 연결 테스트로 검증함(2026-11-01, OrbStack): agent 컨테이너가 기본 Ollama 바인딩(127.0.0.1:11434) 그대로 `host.docker.internal`로 연결에 성공했고, Ollama를 0.0.0.0으로 재바인딩하거나 LAN에 노출할 필요가 없었다. Docker Desktop 등 다른 런타임에서는 호스트 브리지 동작이 달라 재검증이 필요할 수 있다. Docker socket과 호스트 홈 디렉터리는 마운트하지 않는다.

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

## 14. v0.2 방향: 다중 규칙·탐색 (AI DAST)

v0.1은 검증 규칙이 doc-cross-read-v1 하나뿐이고 대상도 고정된 몇 개 경로였다. 이 프로젝트의 목표(README)는 "웹 애플리케이션 보안 검증 도구"이므로, 다음 확장 없이는 인가 비교 테스트 한 종류에 머문다. 대상은 계속 사용자 소유의 로컬 labapp이며, 외부 시스템이나 제3자 자산은 범위에 포함하지 않는다.

v0.2는 두 단계로 나눈다. **M6a**(이 섹션의 확정 설계, 바로 구현)는 기존 typed_args 구조를 바꾸지 않고 추가 가능한 세 규칙(비인증 접근·수직 권한 상승·수동 점검)과 그에 필요한 labapp 확장만 다룬다. **M6b**(§14.7, 설계 방향만)는 탐색(crawl)과 반사형 입력(XSS/SQLi)처럼 Action/Policy 계약 변경이나 새 컴포넌트가 필요한 항목을 M6a 완료 후 별도로 결정한다.

### 14.1 검증 규칙 확장 (M6a)

Verifier(§10)의 `doc-cross-read-v1`과 같은 자리에, 서로 독립된 규칙 세 개를 추가한다. 각 규칙은 자신의 `rule_id`를 가지고, 자신의 confirmed/rejected/inconclusive 조건과 필요한 Evidence 종류를 정의한다. 하나의 Observation이 여러 규칙의 입력이 될 수 있다.

#### unauth-access-v1 (비인증 접근)

- confirmed 조건: 로그인 세션 없이 보낸 요청이, 인증 상태에서만 보이던 데이터와 동일한 본문을 반환.
- **세션 없는 요청은 Planner가 제안하지 않는다.** account_ref는 여전히 "A"/"B"만 허용하는 기존 enum을 그대로 둔다(스키마·Policy 변경 없음). 대신 agent가 code로 직접 수행한다: 어떤 계정이 `/api/public/docs/{id}`(§14.3 신규 엔드포인트)를 성공적으로(200) 읽은 Observation이 생기면, 같은 사이클 안에서 agent가 **쿠키 없는 별도 요청 1회**를 그 URL에 자동으로 보낸다. 이는 bootstrap과 같은 성격의 code-driven 단계이며, Judge의 판단을 거치지 않는다.
- 이 요청은 HTTP 예산에 포함되고, 계정당 1회가 아니라 **URL당 1회**로 중복 방지한다(§8 재실행 규칙의 URL+method 키에서 account_ref를 뺀 버전).
- Evidence의 AccountRef 필드는 이 경우 빈 문자열로 기록한다(어떤 계정도 아님을 명시).

#### vertical-privesc-v1 (수직 권한 상승)

- **새 계정을 만들지 않는다.** 기존 A/B 두 계정을 그대로 쓰되, 실습 앱 내부에서 A는 admin 역할, B는 일반 역할로 고정한다(§14.3). config의 `accounts`는 여전히 정확히 A·B 두 개다.
- confirmed 조건: B(일반)가 관리자 전용 엔드포인트(`/api/admin/users`)에 접근해, A(admin)가 같은 엔드포인트에서 받은 것과 같은 모양의 데이터를 받음. doc-cross-read-v1과 같은 대조 구조(baseline=A의 정상 응답, comparison=B의 응답)를 그대로 재사용한다.
- rejected 조건: B가 403을 받음(소유권 증거 없이 상태 코드만으로 confirm하지 않는다는 §10 원칙은 여기서도 유지 — 반드시 A의 baseline과 B의 comparison이 같은 모양의 데이터를 반환해야 confirm).
- 이 규칙은 **Planner가 정상적으로 제안 가능한 일반 Action**이다(기존 exposed_refs 메커니즘으로 `/api/admin/users` 링크가 노출되면 그 URL을 citing해서 제안). 새 컴포넌트·스키마 변경 없음.

#### passive-exposure-v1 (수동 점검: 헤더·정보노출)

- 능동 요청을 전혀 만들지 않는다 — **이미 수집된 Evidence만** 재검사한다(새 Observation도, 예산 소모도 없음). Verifier가 아니라 "확장된 수동 검사기"로 보되, 자리는 Verifier 규칙과 동일하게 둔다.
- 신호: 응답 헤더에 labapp이 vulnerable 모드에서만 심은 `X-Debug-Backend` 헤더가 있는지, 본문에 고정된 패턴 목록(예: `/Users/`, `stack trace`, `.env`) 중 하나가 있는지. 둘 다 결정론적 문자열/헤더 매칭이며 모델 판단이 아니다.
- outcome: 신호가 있으면 confirmed, 없으면 rejected(해당 run에서 수집된 Evidence 전체 기준). confirmed라도 severity는 `info`(§14.2) — 능동 검증 없는 발견은 위협도를 낮게 매긴다.

### 14.2 Finding 심각도와 다중 규칙 보고

- Finding(§6)에 `severity`(critical/high/medium/low/info)를 추가한다. 규칙별 고정 매핑이며 코드가 정하고 모델은 관여하지 않는다:

  | rule_id | confirmed 시 severity |
  | --- | --- |
  | `doc-cross-read-v1` | high |
  | `unauth-access-v1` | high |
  | `vertical-privesc-v1` | critical |
  | `passive-exposure-v1` | info |

- Finding에는 이미 `VerificationID`를 통해 `Verification.RuleID`가 간접 연결돼 있으니 별도 `rule_id` 필드는 추가하지 않는다(중복 저장 대신 report 생성 시 join).
- report(JSON/Markdown)는 규칙별로 섹션을 나눠, 어떤 규칙이 몇 번 confirmed/rejected/inconclusive였는지 요약한다(기존 `report.RuleSummary`를 그대로 확장; 이미 rule_id별 집계 구조로 돼 있어 변경이 작다).

### 14.3 labapp 확장 (M6a)

새 규칙을 검증하려면 실습 앱에 해당 시나리오가 필요하다(§10과 동일하게 전역 Mode: vulnerable/fixed 하나로 토글. 시나리오별 별도 모드 플래그는 두지 않는다).

- **역할**: `Credential`에 `Role`(`"admin"`/`""`) 필드 추가. seed에서 계정 A에 `admin`, B는 기본값. 계정 매핑(u-a=admin, u-b=일반)은 이미 존재하는 순서를 그대로 쓴다.
- **관리자 엔드포인트**: `GET /api/admin/users` 추가. `fixed` 모드에서는 role이 admin이 아니면 403. `vulnerable` 모드에서는 로그인 여부만 확인(role 무시).
- **무인증 엔드포인트**: `GET /api/public/docs/{id}` 추가(기존 `getDoc`과 별개 핸들러, 세션 쿠키 자체를 보지 않음). `vulnerable` 모드에서는 `getDoc`과 동일한 소유권 규칙으로 문서를 반환, `fixed` 모드에서는 항상 401.
- **디버그 헤더**: 응답 헤더에 `vulnerable` 모드에서만 `X-Debug-Backend: labapp-internal`을 추가(모든 응답 공통 미들웨어; `noStore`와 같은 자리).
- 평가기 전용 `Answer` 구조체(labapp.go)에 역할·무인증 엔드포인트 기대값 필드를 추가한다. agent와 Verifier는 여전히 이를 읽지 않는다.

### 14.4 적용 순서 (M6a)

1. labapp 확장 (§14.3): 역할, 관리자 엔드포인트, 무인증 엔드포인트, 디버그 헤더. 기존 doc-cross-read 시나리오·평가기 분리 유지.
2. `model.Finding.Severity`, 규칙→심각도 매핑, report 집계 확장 (§14.2).
3. Verifier에 `passive-exposure-v1` 추가 — 새 요청이 없어 가장 단순하고, 기존 Evidence 재검사 로직만으로 fixture 테스트 가능.
4. Verifier에 `vertical-privesc-v1` 추가 — doc-cross-read-v1의 대조 구조를 재사용. fake Planner(LabHeuristic 확장)로 A/B가 관리자 엔드포인트를 읽는 경로를 추가.
5. `unauth-access-v1`과 agent의 code-driven 무인증 재조회 단계 추가 — 세 규칙 중 유일하게 agent.go에 새 분기가 필요하다.
6. 각 단계는 fake Planner/Judge로 모델 없이 결정론적으로 먼저 통과시킨 뒤(M3 원칙), 실제 qwen3:8b로 재확인(M4/M5 원칙).

완료 기준: 세 규칙 각각 독립된 fixture/통합 테스트로 confirmed/rejected 경로를 확인하고, 실제 모델로 취약/수정 모드 각 1회 이상 재현한다.

### 14.5 범위 밖 (M6a가 아닌 것)

다음 둘은 M6a에서 하지 않는다. M6a 완료 후 각각을 별도로 검토한다: 탐색(§14.6)과 반사형 입력(§14.7).

### 14.6 탐색(Crawl) — M6b, 구현됨(축소된 형태)

Planner가 다음 행동을 제안하려면 먼저 앱의 표면을 알아야 한다. 현재는 start_path 응답과 로그인 이후 노출된 링크만 관찰 대상이다.

- 파라미터 수집: `internal/extract`가 HTML 응답의 form input name과 href/action의 쿼리 문자열 키를 결정론적으로 뽑아 `Observation.discovered_params`에 기록한다(모델이 만든 파라미터명은 쓰지 않음). form의 action URL도 `exposed_refs`에 추가된다.
- **설계와 달라진 점**: 별도 `internal/crawl` 패키지(큐·frontier·방문 집합을 독립 관리)는 만들지 않았다. §14.1 세 규칙과 마찬가지로, reflected-input-v1(§14.7)이 실제로 필요로 하는 것은 discovered_params뿐이었고, "URL당 방문 여부"는 이미 §8의 `requestKey`+`execs` 맵(반복 행동 방지)이 하는 일과 같아 별도 컴포넌트 없이 재사용했다. Planner는 기존 `exposed_refs` 메커니즘으로 새 URL을 자유롭게 제안할 수 있으므로, 탐색 전용 새 Action 유형이나 큐도 추가하지 않았다.
- M6a의 세 규칙과 이 규칙은 모두 탐색 없이(홈 페이지 하나의 응답에 노출된 링크·폼만으로) 동작한다. 더 넓은 표면 탐색이 실제로 필요해지면 그때 별도 frontier 컴포넌트를 추가한다.

### 14.7 반사형 입력(XSS/SQLi) — M6b, 구현됨(ADR-0002 승인됨)

ADR-0002(`docs/adr/0002-reflected-input-param-value.md`)로 설계를 확정했다. v0.1·M6a까지의 "model은 URL·ID·파라미터 값을 못 만든다"는 핵심 방어선은 그대로 유지하고, 모델이 쓰는 값은 `param_value` 리터럴이 아니라 **고정된 닫힌 집합에서 고르는 `marker_id`**뿐이다(실제 문자열은 `internal/policy`가 code 상수로만 가진다). 파라미터 이름도 §14.6의 `discovered_params`에 있는 것만 허용한다(모델이 지어낼 수 없음).

- `model.MarkerID` enum(`"xss-*"`/`"sqli-*"` 접두사, `"benign-v1"` 포함) + `model.KnownMarkerIDs`.
- `model.HTTPArgs.ParamName`/`MarkerID` 추가, `model.ValidateAction`이 `param_name ∈ discovered_params`와 `marker_id ∈ KnownMarkerIDs`를 검증.
- `policy.MarkerValue`가 marker_id를 실제 페이로드 문자열로 치환(Policy의 고정 맵, config·모델 입력으로 확장 불가); `Policy.CheckAction`이 모르는 marker_id를 거부.
- `internal/tools/http`가 요청을 보내는 마지막 순간에만 쿼리 문자열에 그 값을 주입한다.
- confirmed 조건(`internal/verifier/reflected.go`): `xss-*` 마커의 리터럴 페이로드가 응답 본문에 변형 없이 그대로 나타나거나, 고정된 DB 오류 패턴 목록과 일치(`sqli-*`). `benign-v1`은 메타문자가 없어 그 자체로는 절대 confirm하지 않는다.
- labapp에 `GET /api/search?q=...` 추가: vulnerable 모드는 `q`를 그대로 반사, fixed 모드는 `html.EscapeString`으로 이스케이프. 홈 페이지에 검색 폼(`input name="q"`) 추가.
- method는 GET만, 저장형·2차 공격은 v0.2에서도 범위 밖(ADR-0002 §4 그대로).

완료 기준: fake Planner/Judge로 confirmed(취약 모드)·rejected(수정 모드)·benign marker 단독으로는 confirm 안 함·미발견 파라미터명 거부, 네 가지를 통합 테스트(tests/integration/m6b_test.go)로 확인함. 실제 qwen3:8b로도 확인함(tests/integration/m6b_live_test.go, 2026-11-07) — 안전성(수정 모드 false confirm 0건)은 충족. 단, `discovered_params`가 매 턴 프롬프트에 노출되고 시스템 프롬프트에 명시적 지시까지 추가했음에도 4회 연속 실행 모두 reflected-input-v1을 전혀 시도하지 않았다(상세 경과는 tasks.md M6b 참고). 8B급 소형 로컬 모델이 모델-주도 탐색(§14.1 vertical-privesc-v1도 부분적으로 겪은 문제)에서 겪는 역량 한계로 기록하고, 이 시점에서는 재설계하지 않고 보류한다 — 다음에 다시 볼 때는 unauth-access-v1처럼 code-driven으로 바꾸는 안(ADR-0002에서 한 번 기각했던 대안)을 재검토할 수 있다.

이 확장 전체는 사용자 소유의 로컬 labapp만을 대상으로 하며, 범위 밖 항목(§1)은 v0.2에서도 그대로 유지한다.
