# 공개 API 확장 후 로컬 측정

2026-10-15, 구현 `254b4ac` 기준. 기존 세 원격 호스트의 historical PoC 실험과
별개인 **한 로컬 호스트의 smoke benchmark**다. Ryzen 7 5825U, linux/amd64,
Go 1.27.1, GOMAXPROCS=1, 조건당 150ms × 3회 중앙값. CPU affinity 고정이나
주파수 제어는 하지 않았고 실행 순서도 무작위화하지 않았다. 통계적 유의성이나
전체 Watch 성능을 주장하는 측정은 아니다. 두 suite는 순서대로 실행했다.

코드: [root benchmark](../../../../benchmark_test.go),
[CEL benchmark](../../../../cel/benchmark_test.go).
원시 출력: [reader.txt](reader.txt), [cel.txt](cel.txt).
환경·소스 hash: [metadata.json](metadata.json).

## 직접 읽기

`google.protobuf.Type`의 선두 `name`만 읽는다. child 64개 조건은 각각 64-byte
name을 가진 `Field` 메시지 64개가 뒤따른다. descriptor 준비와 입력 생성은
제외하고 매번 reader 생성 또는 Reset/Pool Get/Put 비용을 포함했다.

| 방식 | child 0개 ns/op | child 64개 ns/op | child 64개 B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| 표준 Unmarshal 후 name | 157.7 | 10,026 | 15,520 | 137 |
| pwr 기본 last | 157.8 | 2,253 | 272 | 2 |
| pwr unique | 158.3 | 161.3 | 272 | 2 |
| pwr Reset + unique | 94.09 | 93.11 | 0 | 0 |
| pwr Pool + unique | 128.6 | 122.8 | 0 | 0 |

기본 모드는 뒤의 tag들을 훑지만 child body를 디코딩하지 않는다. unique는
첫 필드에서 반환하므로 뒤의 child 수에 거의 영향을 받지 않았다. **singular
scalar 중복 없음이라는 입력 계약이 필요한 수치**다. 필드가 뒤에 있거나
누락된 경우 이 조기 반환 이득은 사라진다. 아주 작은 메시지는 새 reader를
매번 만들 때 표준 decoder보다 빠르지 않았다.

## CEL 평가

64개 child가 있는 동일 메시지에서 두 식을 평가했다. CEL compile, schema
준비와 activation map 생성은 제외했으며 이벤트별 activation 값 교체는
포함했다. `native`는 기본 cel-go provider/adapter, `adapter`는 pwrcel이다.
`decoded`만 최초 unmarshal 비용을 제외한다.

| 방식 | name 조건 µs/op | 64개 child 순회 µs/op | name B/op / allocs | 순회 B/op / allocs |
| --- | ---: | ---: | ---: | ---: |
| native, 이미 decoded | 0.260 | 24.136 | 32 / 2 | 4,281 / 68 |
| native, 매번 Unmarshal | 15.054 | 36.573 | 15,553 / 139 | 19,806 / 205 |
| pwrcel, 이미 decoded | 0.674 | 39.569 | 184 / 7 | 10,235 / 332 |
| pwr 기본 last | 2.902 | 62.683 | 488 / 7 | 53,657 / 468 |
| pwr unique | 0.671 | 60.923 | 488 / 7 | 53,657 / 468 |
| pwr Reset + unique | 0.553 | 60.352 | 216 / 5 | 53,129 / 464 |

- name 식: `m.name == "Artifact"`.
- 순회 식: `m.fields.exists(f, f.number == 64)`. 마지막 원소에서 일치하므로
  모든 child를 방문한다. 이 경우 현재 adapter의 collection/view 할당과
  중첩 reader 비용 때문에 표준 Unmarshal + CEL보다 느렸다.
- 이미 decoded된 입력에는 기본 cel-go 경로가 가장 저렴했다. pwrcel은
  wire 오류를 presence 검사에서도 전달하는 일반 경로를 쓰고 추가 객체
  변환도 하므로, decoded 입력의 속도를 개선하기 위한 adapter가 아니다.

따라서 bytes에서 일부 필드만 선택해 읽는 경로에 우선 적용한다. 전체 collection
순회나 이미 decoded된 authorized image에는 표준 CEL 재사용 경로가 유리하다.
이번 추가 측정에서는 바이너리 크기나 실제 payday/SQL/권한 비용을 측정하지 않았다.

## 재현

레포 root에서 순서대로 실행한다:

```sh
GOMAXPROCS=1 go test -run '^$' -bench '^BenchmarkName$' -benchmem -benchtime 150ms -count 3
cd cel
GOMAXPROCS=1 go test -run '^$' -bench '^BenchmarkEvaluation$' -benchmem -benchtime 150ms -count 3
```

이후 구현 변경은 동일 명령으로 재측정한다. 기록된 수치는 해당 commit과
benchmark source hash에 대한 결과다.
