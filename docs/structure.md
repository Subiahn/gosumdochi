# 목표 프로젝트 구조

```text
gosumdochi/
├── cmd/
│   ├── gosumdochi/main.go
│   └── labapp/main.go
├── internal/
│   ├── agent/              # 실행 상태 머신과 orchestration
│   ├── planner/            # 관찰 해석, 가설·행동 후보 생성
│   ├── judge/              # 고정 타입 판단 및 출력 검증
│   ├── llm/                # 제공자 중립 계약, Ollama 어댑터
│   ├── policy/             # 대상·행동·예산의 강제 검사
│   ├── executor/           # 도구 호출, 시간 제한, 취소
│   ├── tools/http/         # HTTP 도구와 안전한 transport
│   ├── session/            # 테스트 계정별 인증 정보 격리
│   ├── state/              # 관찰·가설·결정·결과 저장
│   ├── evidence/           # 증거 ID, 마스킹, 해시
│   ├── verifier/           # 대조 실험 결과 검증
│   ├── model/              # 공유 타입 및 버전
│   ├── report/             # JSON 및 Markdown 보고서
│   ├── config/             # 설정 검증, 비밀 참조
│   ├── cli/                # 명령과 종료 코드
│   └── labapp/             # 실습 앱: 고정 데이터·취약/수정 모드
├── configs/
│   ├── lab.example.json
│   └── schemas/            # 설정·도구·판단 JSON Schema
├── deploy/
│   ├── compose.yaml
│   ├── Dockerfile.agent
│   ├── Dockerfile.lab
│   └── .env.example
├── tests/
│   ├── integration/
│   ├── e2e/
│   └── fixtures/           # 모델 응답 fixture, 정답은 평가기 전용
├── specs/001-local-authz-agent/
│   ├── spec.md
│   └── tasks.md
├── docs/
│   ├── SDD.md
│   ├── structure.md
│   └── adr/
├── assets/
├── runs/                   # 생성 파일, Git 제외
├── go.mod
├── Makefile
└── README.md
```

디렉터리는 구현이 생길 때 생성한다. 미래 기능을 위한 빈 Go 패키지는 만들지 않는다.

## 의존성 방향

- cli → config, agent, report
- agent → planner, judge, policy, executor, state, verifier
- planner / judge → llm, model
- executor → tool registry, policy, session, evidence
- tools/http → model 및 주입된 transport
- report → model, state의 읽기 인터페이스
- model은 내부 서비스에 의존하지 않는다.
- llm과 tools는 서로 직접 호출하지 않는다. 모델은 네트워크 실행 권한을 갖지 않는다.

## 기존 starter 이전 계획

| 기존 | 변경 |
| --- | --- |
| internal/engine/plan.go | agent orchestrator 도입 후 대체; 설정 미리보기는 CLI plan으로 유지 |
| internal/checks/check.go | 내부 도구 계약으로 대체; 기존 인터페이스는 사용처 제거 후 삭제 |
| internal/ai/provider.go | llm 제공자 계약으로 대체 |
| internal/model/result.go | 실행·증거·가설 모델로 확장 |
| internal/report/json.go | 새 버전 결과 형식으로 변경 |
| reports/ | 신규 실행 결과는 runs/<run-id>/ 아래 저장 |

이전 후 plan은 설정만 검증하고, run은 실제 에이전트를 실행한다. 명령 의미를 혼용하지 않는다.
