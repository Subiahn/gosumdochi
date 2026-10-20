# Go슴도치 — 설계 패키지 v0.1

상태: 구현 전 설계 초안 · 2026-10-18

기준 환경: Apple M5 / 통합 메모리 16GB / macOS / Docker.
목표: 로컬 실습 앱에서 AI가 탐색하고 접근 권한 가설을 세운 뒤 두 테스트 계정으로 검증하고 증거가 연결된 보고서를 생성한다.

읽는 순서:
1. docs/structure.md — 목표 프로젝트 구조
2. docs/SDD.md — Software Design Description, 시스템 설계 명세
3. specs/001-local-authz-agent/spec.md — 행동·수용 기준
4. specs/001-local-authz-agent/tasks.md — 구현 순서
5. docs/adr/0001-local-first.md — 주요 결정

여기서 SDD 문서는 설계 명세를 뜻한다. Spec-Driven Development 방식으로도 사용할 수 있도록 별도의 spec과 tasks를 함께 제공한다.
기존 starter 코드를 재배치하거나 실행 기능을 구현한 패키지가 아니다. 이 설계가 이전 스캐너 중심 architecture.md를 대체하며, 실제 반영은 구현 단계에서 수행한다.
