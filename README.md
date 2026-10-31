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

M0~M5(`specs/001-local-authz-agent/tasks.md`)까지 구현됨: 계약·실습 앱·실행 경계·결정론적 에이전트 루프·Ollama 연동·보고서. 현재 검증된 범위는 계정 간 문서 교차 접근(수평 권한 비교) 하나뿐이다. 더 넓은 탐색과 다른 취약점 규칙은 `docs/SDD.md` §14(v0.2, 설계만)에 적어 두었고 아직 구현하지 않았다.

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

## 알려진 한계

- **검증 규칙 하나**: `doc-cross-read-v1`(계정 간 문서 교차 읽기)만 구현됨. 범용 취약점 스캐너가 아니다.
- **탐색 없음**: 설정에 적힌 시작 페이지·로그인·신원 경로만 관찰하고, 응답에 노출된 링크만 따라간다. 별도 크롤러는 없다(`docs/SDD.md` §14.1 설계만 존재).
- **작은 로컬 모델은 스캐폴딩에 민감하다**: qwen3:8b(8B) 자체의 추론 능력보다, Planner/Judge에게 정확히 무엇이 필요한지(스키마의 실제 제약, Verifier의 판단 사유)를 구조화해서 보여주는지가 결과를 더 크게 갈랐다(위 "실제 모델 평가 결과" 참고). 다른 모델이나 더 복잡한 시나리오에서는 또 다른 간극이 나올 수 있다.
- **macOS 배터리 절전**: 배터리 사용 중에는 macOS의 Sleep Service/Power Nap이 15~20분 주기로 개입해 `caffeinate -ims`로도 막히지 않는다(코드의 타임아웃 로직과는 무관; `pmset -g log`로 확인 가능). 정확한 시간 측정이 필요하면 전원을 연결하고 화면을 켠 채로 실행한다.
- **Docker → 호스트 Ollama 경로 미확인**: 이 평가는 호스트에서 바로 실행했다. `deploy/compose.yaml`의 agent 컨테이너가 `host.docker.internal`로 호스트 Ollama에 닿는지는 아직 확인하지 않았다(M1 남은 항목).
- **재개 불가**: 중단된 run을 이어서 계속 실행하는 기능은 없다. `report`로 그 시점까지의 기록만 다시 볼 수 있다.