# 🦔 Go슴도치 · Goseumdochi

**Go 기반 AI 웹 애플리케이션 보안 검증 도구**

Go슴도치는 웹 애플리케이션을 탐색하고, AI가 제안한 보안 가설을 실제 HTTP 요청과 응답으로 검증하는 프로젝트입니다.

단순히 점검 결과를 나열하는 데서 나아가, 애플리케이션의 동작과 사용자별 접근 권한을 비교하여 보안 문제를 찾고 재현 가능한 증거를 남기는 것을 목표로 합니다.

## 주요 기능

- **애플리케이션 탐색**: 페이지, 엔드포인트, 요청 파라미터와 인증 흐름 수집
- **AI 기반 가설 생성**: 수집한 정보를 바탕으로 보안 문제의 가능성과 추가 검증 방법 제안
- **계정 간 권한 비교**: 서로 다른 두 계정으로 동일한 리소스에 접근하여 접근 통제 문제 검증
- **증거 기반 결과 정리**: 확인된 문제와 추가 확인이 필요한 가설을 구분하고 요청·응답 근거 기록
- **보고서 생성**: 검증 과정, 재현 절차와 결과를 보고서로 정리

## 동작 흐름

대상 설정 → 애플리케이션 탐색 → 보안 가설 생성 → 검증 실행 → 증거 수집 → 보고서 생성

Go 실행 엔진이 탐색과 검증을 담당하고, AI는 수집한 정보를 해석하여 가설과 다음 검증 단계를 제안합니다. 취약점 판단은 실제 검증 결과와 증거를 바탕으로 합니다.

## 개발 방향

- Go 기반 탐색·검증 엔진
- Ollama 등을 통한 로컬 LLM 연동
- 구조화된 판단 결과를 활용한 검증 우선순위 및 실행 경로 결정
- Docker 기반의 분리된 실습 환경

## 프로젝트 상태

M0~M5, M6a, M6b(`specs/001-local-authz-agent/tasks.md`)까지 구현됨: 계약·실습 앱·실행 경계·결정론적 에이전트 루프·Ollama 연동·보고서, 그리고 다섯 개 검증 규칙(아래). `docs/SDD.md` §14.6(탐색/crawl)은 실제로는 §8의 기존 반복-행동 dedup으로 충분해 별도 컴포넌트 없이 흡수됐고, 반사형 입력 하나만 ADR-0002로 설계를 확정한 뒤 구현했다.

현재 구현된 검증 규칙:

| rule_id | 확인하는 것 | severity | Planner 제안 방식 |
| --- | --- | --- | --- |
| `doc-cross-read-v1` | 계정 간 문서 교차 읽기(수평 권한) | high | 모델이 제안 |
| `vertical-privesc-v1` | 일반 계정이 admin 전용 엔드포인트 접근 | critical | 모델이 제안 |
| `unauth-access-v1` | 세션 없이도 보호된 데이터 접근 | high | code-driven(모델이 제안하지 않음) |
| `passive-exposure-v1` | 디버그 헤더·정보 노출 패턴 | info | 수동 점검(새 요청 없음) |
| `reflected-input-v1` | 반사형 XSS/SQLi 신호 | high | 모델이 고정된 마커 id 중 하나를 선택(ADR-0002) |

## 사용 범위

본인이 소유하거나 명시적으로 점검 허가를 받은 환경에서 사용합니다. 초기 개발과 검증은 로컬 실습 애플리케이션(`cmd/labapp`)을 대상으로 진행합니다. 평가기(테스트)만 정답과 앱 모드를 알고, 에이전트에는 전달하지 않습니다.

## 실행 방법

사전 준비:

```bash
go build -o gosumdochi ./cmd/gosumdochi
go build -o labapp ./cmd/labapp
ollama pull qwen3:8b   # 또는 configs/*.json의 llm.model에 맞는 모델
```

실습 앱 기동 (다른 터미널, 또는 `-config`가 가리키는 주소에 맞춰):

```bash
LAB_MODE=vulnerable \
LAB_A_USER=alice LAB_A_PASS=<pw> \
LAB_B_USER=bob   LAB_B_PASS=<pw> \
LAB_ADDR=127.0.0.1:18080 \
./labapp
```

설정 파일(`configs/lab.example.json` 참고)에서 `target_origin`, `allowed_lab_cidrs`, `llm.base_url`을 실제 주소에 맞게 바꾼다. 계정 비밀은 설정 파일에 직접 넣지 않고 `env:NAME` 또는 `file:PATH` 참조만 쓴다.

```bash
# 1. 설정만 검증 (네트워크 없음)
./gosumdochi plan -config configs/lab.example.json

# 2. 에이전트 실행 (run.json/events.jsonl/evidence/report.* 가 output_dir/<run-id>/ 에 쌓임)
LAB_A_USER=alice LAB_A_PASS=<pw> LAB_B_USER=bob LAB_B_PASS=<pw> \
  ./gosumdochi run -config configs/lab.example.json -run-id my-run-1

# 3. 저장된 run에서 보고서만 다시 생성 (네트워크 없음, 몇 번을 다시 돌려도 report.json 내용은 동일)
./gosumdochi report -run runs/my-run-1
```

종료 코드: `0`=완료(발견 여부와 무관), `1`=설정·인프라 실패, `2`=예산·모델 오류 등 불완전 실행, `130`=Ctrl+C 취소. 발견 여부는 종료 코드가 아니라 `report.json`의 `findings`로 판단한다. 같은 `-run-id`를 두 번 쓰면(이전 run이 남아 있으면) `run`은 바로 실패한다 — 다른 id를 쓰거나 해당 `runs/<id>/` 디렉터리를 지운다.

### Docker로 실행

labapp은 항상 컨테이너로, agent는 선택적으로 컨테이너로 돌릴 수 있다(`deploy/compose.yaml`). Ollama는 Mac 호스트에서 그대로 실행한다 — 바인딩을 바꾸거나 네트워크에 노출할 필요 없음(OrbStack 기준, 아래 "알려진 한계" 참고).

```bash
cp deploy/.env.example deploy/.env   # 값 채우기 (git-ignored)
cd deploy
docker compose up -d labapp
docker compose --profile agent run --rm agent plan -config /configs/lab.example.json
docker compose --profile agent run --rm agent run  -config /configs/lab.example.json -run-id my-run-1
# 결과는 호스트의 ../runs/my-run-1/ 에 쌓인다 (compose.yaml의 volumes)
docker compose down
```

`configs/lab.example.json`의 `target_origin`은 서비스명 `labapp`을, `llm.base_url`은 `host.docker.internal`을 가리킨다. `lab` 네트워크 서브넷은 이 설정의 `allowed_lab_cidrs`(172.28.0.0/16)와 맞게 `compose.yaml`에서 고정해 뒀다.

### 테스트 실행

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...        # fake Planner/Judge로 하는 결정론적 테스트. Ollama 불필요
```

실제 Ollama 모델을 Planner/Judge로 써서 돌리는 통합 테스트(`tests/integration/m*_live_test.go`)는 `OLLAMA_LIVE_MODEL` 환경변수가 없으면 자동으로 스킵된다. 값을 주면 그 모델로 labapp을 인-프로세스(`httptest.Server`)로 띄워 실제 Planner/Judge 호출까지 전부 돌린다 — 네트워크는 로컬 Ollama(`http://127.0.0.1:11434`)로만 나간다.

```bash
ollama pull qwen3:14b   # 처음 한 번
OLLAMA_LIVE_MODEL=qwen3:14b go test -count=1 -v -timeout 900s -run 'TestLiveM6b' ./tests/integration/...
```

- 모델 하나당 한 사이클에 여러 번 모델을 호출하므로 실행 시간이 길다(관찰상 qwen3:8b는 2~6분, qwen3:14b는 5~13분/테스트). `-timeout`을 충분히 주고, 여러 Live 테스트를 한 번에 묶어 돌릴 때는(`-run 'TestLiveM6b'`처럼 여러 개를 매칭) 그 합계만큼 넉넉히 잡는다 — 테스트 하나가 느려지면 같은 바이너리의 나머지가 패키지 전체 타임아웃에 걸려 넘어갈 수 있다.
- `TestLiveM4*`/`TestLiveM5*`: doc-cross-read-v1 단독. `TestLiveM6aVulnerableMode`/`FixedMode`: vertical-privesc-v1·unauth-access-v1·passive-exposure-v1. `TestLiveM6bVulnerableMode`/`FixedMode`: 위 전부 + reflected-input-v1.
- 수정 모드(`ModeFixed`) 테스트는 "거짓 confirm이 0건이어야 한다"를 하드 단언한다. 취약 모드 테스트 중 모델이 스스로 찾아야 하는 가설(`doc-cross-read-v1`, `vertical-privesc-v1`, `reflected-input-v1`)은 로그만 남기고 단언하지 않는다 — 작은 로컬 모델이 매 실행 그걸 찾아내는지는 "코드가 맞는가"가 아니라 "모델이 할 수 있는가"의 질문이기 때문이다.

## 실제 모델 평가 결과 (qwen3:8b, M5)

2026-10-31, 16GB M-series Mac, host(비-Docker) 환경에서 취약/수정 모드 각 5회. `temperature=0, seed=42`로 각 모드 내 5회가 서로 결정론적으로 동일하다(같은 cycles/model_calls/http_requests).

| mode | run | status | stop_reason | cycles | model_calls | http_requests | H-1 | 실측 시간 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| vulnerable | 1~5 | completed | goal_verified | 9 | 18 | 14 | **confirmed** | 268~376초 |
| fixed | 1~5 | completed | exploration_exhausted | 4 | 9 | 9 | rejected | 130~270초 |

- **취약 모드 5/5 confirmed** (목표 4/5 이상 달성). **수정 모드 5/5 중 0회 false confirmed**(모두 정확히 rejected, AC-02 충족).
- **Mac peak 메모리**: `gosumdochi` 프로세스 자체는 7.3~10.4 MiB(`time -l`의 peak memory footprint). 무거운 연산은 agent 프로세스가 아니라 별도의 Ollama 서버 쪽에서 일어난다.
- **원인 분석과 수정 과정** (SDD §12 "프롬프트·모델·도구 문제 분리"): 첫 시도는 취약 모드 0/5, 수정 모드도 0/5(엉뚱한 이유로 전부 실패)였다. 아래 세 가지를 순서대로 찾아 고친 뒤에 위 결과에 도달했다.
  1. **Judge가 `selected_action_id`를 비움** → 매 실행이 즉시 `model_error`. JSON Schema의 조건부 필수(`if/then`)를 Ollama의 구조화 출력(grammar 기반)이 지키지 않는 게 원인. `selected_action_id`를 무조건 필수 필드(미실행 시 빈 문자열)로 바꿔 해결(`configs/schemas/judge-decision.schema.json`, `internal/judge/ollama.go`).
  2. **Planner가 허용치(3개)보다 많은 후보를 내고 재시도에서도 똑같이 반복** → `model_error`. Planner에 보내는 스키마의 `candidates.maxItems`가 실제 예산과 무관하게 5로 고정돼 있던 게 원인. 호출마다 실제 예산 값으로 스키마를 좁히도록 수정(`internal/planner/ollama.go`의 `schemaForLimit`). 이 수정만으로 수정 모드는 5/5 정상 완료됐지만, 취약 모드는 여전히 0/5였다 — 모델이 B 계정으로 `/api/docs`를 한 번도 다시 호출하지 않아 B 자신의 문서(대조군)를 못 구했기 때문.
  3. **Planner가 Verifier의 판단 사유를 못 봄** → Judge에게는 `Verification.Reasons`(예: "B reads A's document: no control: reader's own document response")를 주는데 Planner에게는 안 주고 있었다. 프롬프트로 일반 규칙("계정별로 먼저 자기 데이터를 봐라")을 설명해도 안 통했지만, Verifier가 이미 계산해둔 구체적 결핍 사유를 그대로 보여주자(`planner.Input.Verifications`, `internal/planner/ollama.go`) 모델이 바로 B 계정으로 자기 문서를 조회했고, 그걸로 confirm까지 도달했다. 세 수정 중 가장 결정적이었다.
- 이 평가의 전체 이벤트 로그와 report.json/md는 `runs/<run-id>/`에 재생성 가능하게 남아 있다(이 세션의 원본은 커밋에 포함하지 않음).

## 실제 모델 평가 결과 (qwen3:14b, M6a+M6b)

2026-11-22, 같은 Mac에서 다섯 규칙 전부를 동시에 켜고 평가. 처음에는 `vertical-privesc-v1`·`reflected-input-v1`을 qwen3:8b가 한 번도 못 찾아서 "소형 모델의 역량 한계"로 결론 냈었는데, qwen3:14b로 바꿔도 똑같았던 게 실마리가 되어 다시 파보니 실은 code의 버그 두 개였다(모델 교체가 아니라 버그 수정으로 해결):

1. **스키마 버그**: Ollama의 구조화 출력이 *선택적(optional)* 필드의 enum 제약은 강제하지 않는다. `marker_id`가 필수 필드가 아니었던 탓에, 모델이 마커를 고르려 해도 실제로는 매번 빈 문자열이 나가 조용히 거부당하고 있었다. `param_name`/`marker_id`를 필수 필드로 바꾸고, "이번엔 안 씀"을 뜻하는 `marker_id="none"`을 enum에 추가해서 해결.
2. **전이 버그**: 위 수정 후 모델이 실제로 여러 마커를 순서대로 시도하게 되자, 안 되는 마커 먼저(rejected) → 되는 마커 나중(confirmed)인 경우가 실제로 나왔는데, `rejected`에서 다른 상태로 못 가게 막아놔서 전체 run이 패닉으로 죽었다. `rejected → testing` 재오픈 전이를 허용해서 해결.

수정 후 취약 모드에서 `H-PRIVESC`·`H-UNAUTH`·`H-PASSIVE`·`H-REFLECTED` 네 가설이 모두 confirmed됐고, 수정 모드 안전성(거짓 confirm 0건)은 그대로 유지됐다. 상세 경과는 `specs/001-local-authz-agent/tasks.md`의 M6a/M6b 절 참고.

### report.md 예시

같은 평가에서 나온 실제 `report.md`를 축약한 것(반복되는 `passive-exposure-v1` Verification 줄은 일부만 남김). 모든 주장이 `V-xxxx`/`E-xxxx` id로 추적 가능하다는 걸 보여주는 게 목적이다.

```markdown
## Findings

### [INFO] F-0001 — Responses exposed internal information
Collected responses contained a debug header or an information-disclosure
pattern that should not reach a client.
- verification: `V-0004`
- evidence: `E-0001`, `E-0002`, `E-0003`, `E-0004`
- limitations: Rule passive-exposure-v1 is a passive check over evidence
  already collected; it does not by itself indicate an exploitable vulnerability.

### [HIGH] F-0002 — A resource was reachable with no session at all
In the lab app, a request sent without any login session returned data
that should require authentication.
- verification: `V-0008`
- evidence: `E-0006`, `E-0007`

### [HIGH] F-0003 — A parameter reflected an unescaped payload or surfaced a database error
A code-chosen marker value, injected into a discovered parameter, came
back unescaped in the response or triggered a database error pattern.
- verification: `V-0013`
- evidence: `E-0010`
- limitations: Rule reflected-input-v1 only tried a fixed, closed set of
  markers (ADR-0002); it did not try every possible payload.

### [CRITICAL] F-0004 — A regular account could access the admin-only user list
In the lab app, an account without the admin role retrieved the same data
an admin-only endpoint returns to an admin account.
- verification: `V-0014`
- evidence: `E-0005`, `E-0011`

## Rule summary
| rule_id | confirmed | rejected | inconclusive |
| --- | --- | --- | --- |
| doc-cross-read-v1 | 0 | 0 | 2 |
| passive-exposure-v1 | 10 | 0 | 0 |
| reflected-input-v1 | 1 | 0 | 1 |
| unauth-access-v1 | 1 | 0 | 1 |
| vertical-privesc-v1 | 1 | 0 | 2 |

## Verifications (일부)
| id | hypothesis | outcome | rule_id | reasons |
| --- | --- | --- | --- | --- |
| V-0008 | H-UNAUTH | confirmed | unauth-access-v1 | a request with no session returned the same document (same marker) an authenticated read showed |
| V-0013 | H-REFLECTED | confirmed | reflected-input-v1 | marker xss-attr-v1 was reflected unescaped in the response |
| V-0014 | H-PRIVESC | confirmed | vertical-privesc-v1 | the regular account received the same admin user list the admin account gets |

## Hypotheses
| id | status | claim |
| --- | --- | --- |
| H-1 | inconclusive | One test account can read a document owned by the other test account (missing object-level access control). |
| H-PRIVESC | confirmed | An account without the admin role can reach an admin-only endpoint's data. |
| H-UNAUTH | confirmed | A resource that should require login is reachable with no session at all. |
| H-PASSIVE | confirmed | A response exposes internal information (debug header or info-disclosure pattern). |
| H-REFLECTED | confirmed | A parameter value is reflected unescaped, or triggers a database error (XSS/SQLi signal). |

## Limitations
- Run ended as "incomplete" (stop_reason=budget_exhausted): interrupted
  during planner call. Findings below reflect only what was confirmed
  before that point.
- A finding covers only the documents actually read during this run. It
  does not imply the rest of the application is safe.
- Status codes or identical response bodies alone never confirm a
  finding; confirmation requires matching owner and marker evidence.
```

이번 run은 `H-1`(doc-cross-read-v1)이 예산 소진으로 inconclusive였던 채로 끝났다 — 나머지 네 가설은 confirmed됐지만 모델이 교차계정 문서 읽기까지는 budget(cycles=11) 안에 못 끝냈다. 이 run의 전체 `report.json`/`report.md`는 재생성 가능하지만 커밋에는 포함하지 않는다(위 "실제 모델 평가 결과" 섹션들과 같은 이유).

## 알려진 한계

- **검증 규칙 다섯 개**(위 "프로젝트 상태" 표): 저장형/2차 공격, SQLi 외의 더 깊은 공격 기법, 인증 우회 전반 등은 범위 밖이다. 범용 취약점 스캐너가 아니다.
- **별도 크롤러 없음**: 응답에 노출된 링크·form action만 따라간다(`docs/SDD.md` §14.6). 더 넓은 탐색이 실제로 필요해지면 별도 frontier 컴포넌트를 추가할 수 있다고 적어 뒀지만 아직은 불필요했다.
- **작은 로컬 모델은 스캐폴딩에 민감하다**: qwen3:8b/14b 자체의 추론 능력보다, Planner/Judge에게 정확히 무엇이 필요한지(스키마의 실제 제약, Verifier의 판단 사유)를 구조화해서 보여주는지가 결과를 더 크게 갈랐다. "모델이 못 찾는다"로 보이는 증상이 실은 스키마의 optional 필드 enum 미강제 같은 code 버그였던 사례가 두 번 있었다(위 두 "실제 모델 평가 결과" 참고) — 모델 역량 문제로 결론 내리기 전에 `candidate_rejected` 이벤트로 실제 거부 이유를 먼저 확인하는 걸 권한다.
- **macOS 배터리 절전**: 배터리 사용 중에는 macOS의 Sleep Service/Power Nap이 15~20분 주기로 개입해 `caffeinate -ims`로도 막히지 않는다(코드의 타임아웃 로직과는 무관; `pmset -g log`로 확인 가능). 정확한 시간 측정이 필요하면 전원을 연결하고 화면을 켠 채로 실행한다.
- **Docker 경로는 OrbStack으로만 확인함**: `deploy/compose.yaml`의 agent 컨테이너가 `host.docker.internal`로 호스트 Ollama에, `labapp`에 내부 `lab` 네트워크로 닿는 것을 OrbStack에서 확인했다(위 평가는 호스트에서 직접 실행). `lab` 네트워크 서브넷을 `configs/lab.example.json`의 `allowed_lab_cidrs`와 맞게 172.28.0.0/16으로 고정해 뒀다. Docker Desktop 등 다른 런타임에서는 호스트 네트워킹 동작이 달라 재확인이 필요할 수 있다.
- **재개 불가**: 중단된 run을 이어서 계속 실행하는 기능은 없다. `report`로 그 시점까지의 기록만 다시 볼 수 있다.