# ADR-0002: reflected-input-v1 — 모델이 파라미터 값을 제안할 수 있는 유일한 지점

상태: 제안 · 2026-11-07

## 배경

v0.1~M6a 전체의 핵심 방어선(docs/SDD.md §3, §9)은 "모델은 URL·ID·파라미터 값을 만들 수 없다 — Policy 이전 단계에서 code가 정한 값만 쓸 수 있다"였다. `model.ValidateAction`은 `typed_args.url`이 `source_observation_id`가 가리키는 Observation의 `exposed_refs`에 **문자 그대로 일치**해야만 통과시킨다(internal/model/validate.go:106-111).

반사형 입력(XSS/SQLi) 점검(docs/SDD.md §14.7)은 이 경계를 깨지 않고는 불가능하다 — 점검 대상 자체가 "모델이 고른 값이 응답에 그대로 반사되는가"이기 때문이다. 이 ADR은 그 완화를 **어디까지, 어떻게** 허용할지를 결정한다.

## 결정

1. **완화 범위는 "어떤 마커를 쓸지 고르는 것" 하나로 한정한다.** URL의 베이스 경로, 계정, 메서드(GET 고정), 대상 엔드포인트 자체는 기존과 동일하게 전부 code가 검증한 참조(exposed_refs 등)로만 결정된다. 모델이 쓰는 값은 실제 문자열이 아니라 `typed_args.marker_id`뿐이며, 그 id가 가리키는 문자열은 전적으로 Policy 코드가 정한다 — 모델이 쓴 문자열 자체가 요청에 실리는 경로는 없다.

2. **파라미터 이름도 모델이 지어낼 수 없다.** crawl(§14.6)이 code로 수집한 `discovered_params`(form input name, 쿼리 문자열 키 — 응답에서 결정론적으로 뽑은 값)에 있는 이름만 `typed_args.param_name`으로 쓸 수 있다. `model.ValidateAction`은 `param_name`이 `source_observation_id`가 노출한 `discovered_params` 목록에 있는지 확인한다(exposed_refs 검사와 같은 패턴).

3. **파라미터 값은 자유 문자열이 아니라 고정된 닫힌 집합(markers)에서만 고른다.** 모델은 `marker_id`만 제안하고, Policy가 그 id를 code에 박힌 정확한 문자열로 치환한다. 탐지율을 위해 목록 자체를 실전 페이로드 여러 종으로 넓힌다(각 부류 3~5개 정도):
   - 반사 탐지용(XSS): 길이 제한이 있는 무해한 영숫자 마커(예: `gsdq-marker-7f3a`)부터 `<script>alert(1)</script>`, `"><img src=x onerror=1>`, `'><svg onload=1>` 같은 실제 반사형 XSS 벡터까지.
   - SQLi 신호 탐지용: `'`, `' OR '1'='1`, `' --`, `" OR "1"="1` 등 — DB 오류 패턴이나 쿼리 로직 변형을 유도하는 것이 목적이며, 콘텐츠 자체는 비파괴적이다(DML/DDL 문자열 금지 — `DROP`, `INSERT`, `UPDATE`, `DELETE` 등은 어떤 마커에도 포함하지 않는다).
   - 목록은 Policy 코드에 상수로 박혀 있으며 config나 모델 입력으로 확장할 수 없다. 탐지율이 더 필요해지면 이 상수 목록에 마커를 추가하는 식으로 늘린다 — 모델이 값을 직접 쓰게 허용하는 방향으로는 가지 않는다.

4. **메서드는 GET만 허용한다.** 저장형(stored)·2차(second-order) 공격은 범위 밖이며, v0.2 이후에도 다루지 않는다(docs/SDD.md §1 범위 제한 유지).

5. **요청은 비파괴적이어야 한다.** 각 마커는 읽기 전용 GET 요청의 쿼리 문자열에만 실린다. 실습 앱(labapp)은 이 요청으로 상태를 변경하지 않는다 — vulnerable 모드에서도 반사만 일어나고 DB 쓰기는 없다.

6. **Evidence·마스킹 경로는 그대로 적용한다.** 마커 값도 응답 본문에 그대로 나타날 수 있으므로, 기존 마스킹(비밀값 마스킹)과는 별개 경로이며 마스킹 대상이 아니다(마커는 비밀이 아니다). Evidence에는 어떤 marker_id가 쓰였는지 기록한다.

7. **Verifier 규칙 `reflected-input-v1`의 confirmed 조건**은 응답 본문에 마커 문자열이 **변형 없이** 그대로 나타나거나(XSS 신호), 고정된 DB 오류 패턴 목록과 일치하는 문자열이 나타나는 경우(SQLi 신호)로 한정한다. 둘 다 결정론적 문자열 매칭이며 모델 판단이 아니다(§10 원칙 유지).

## 대안으로 검토했으나 채택하지 않은 안

- **파라미터 값을 완전 자유 문자열로 허용(추가 시그니처로서도)**: 탐지력은 가장 높아지지만 "모델이 값을 못 만든다"는 핵심 불변식을 사실상 무력화하고, Policy가 값을 더 이상 화이트리스트로 검증할 수 없게 된다. 대신 고정 마커 집합 자체를 실전 페이로드 여러 종으로 넓히는 쪽을 택했다 — id 선택이라는 통제된 경로는 유지하면서 탐지력만 끌어올린다.
- **크롤러가 자동으로 모든 발견된 파라미터에 모든 마커를 대입(모델 비관여)**: 완전히 code-driven으로 만들면 §14.1의 unauth-access-v1과 같은 패턴이 되어 더 안전하지만, Planner의 탐색 판단(어떤 파라미터가 테스트할 가치가 있는지)을 완전히 제거한다. 1차 버전은 "모델이 마커 집합에서 고르고 code가 전부 재검증"하는 절충안으로 간다 — 이후 평가에서 모델 기여가 없다고 판단되면 전면 code-driven으로 바꿀 수 있다(되돌리기 쉬운 방향).

## 구현 범위(참고, 결정 사항 아님)

- `model.HTTPArgs`에 `ParamName`, `MarkerID string` 필드 추가(둘 다 생략 시 기존 동작과 동일).
- `model.Refs`에 발견된 파라미터 목록을 노출하는 필드 추가(Observation별 또는 URL별).
- `model.ValidateAction`에 `param_name ∈ discovered_params` 검사 추가.
- `policy.Policy.CheckAction`이 `marker_id`를 고정 맵(Policy 코드 상수, §결정 3)에서 조회해 실제 문자열로 치환하고, 모르는 id는 거부한다.
- `internal/verifier/reflected.go` 신규: 위 7항의 confirmed 조건 구현.
- labapp에 반사형 입력을 받는 테스트용 엔드포인트 추가(예: 검색창 — vulnerable 모드는 이스케이프 없이 반사, fixed 모드는 HTML 이스케이프).

## 상태 전환 조건

이 ADR이 승인되면 M6b의 reflected-input-v1 구현을 진행한다. 승인되지 않으면 M6b는 crawl(§14.6)만 구현하고 reflected-input-v1은 미구현으로 둔다.
