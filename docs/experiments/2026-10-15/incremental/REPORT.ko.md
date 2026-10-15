# 점진적 protobuf wire 스캔 실험

2026-10-15. 사용자가 제안한 “2를 찾으며 지나친 1의 위치를 저장하고, 이후 1은 즉시 찾고, 3은 2 다음부터 검색”을 구현해 세 테스트 머신에서 비교했다. 제품 코드/PR은 변경하지 않았다.

## 판단

점진적 스캔은 동작했고 재검색을 없앴다. map 뒤의 필드2를8번 읽는 direct 비용은 매번 재검색16.66µs에서 점진적 캐시2.19µs로 줄었다. 관심 필드를 미리 아는 기존 한 번 수집은1.94µs여서 이 경우 가장 빨랐다.

반대로 CEL이 앞의 repo 조건에서 탈락하고 뒤의 artifact_type을 읽지 않는 경우에는 기존 수집2.77µs에서 점진적 first0.95µs로 줄었다. 여기서는 실제 요청된 필드까지만 진행하는 효과가 크다. 단 한 필드만 요청되므로 캐시가 없는 단순 first 검색도0.85µs로 빠르다.

필드를 모두 읽는 small4fieldCEL은 기존 first1.80µs, 점진적 first1.88µs였다. 점진적 인덱스는 현재 구현에서 직접 읽기 session당192B/2alloc으로 기존 table112B/1alloc보다80B/1alloc 많다. 모든 필드를 알아서 수집하는 경로의 보편적인 대체재라고 결론내릴 수는 없다.

따라서 실행 중 접근할 필드가 정해지거나 단락 평가로 뒤쪽 필드를 생략하는 경로에는 점진적 스캔이 유용하다. 접근할 필드를 미리 알고 모두 사용할 때는 기존 한 번 수집이 유리했다. first의 이점은 중복 scalar가 없다는 계약에 의존한다. last 모드도 반복 접근의 재검색은 없애지만 첫 필드 요청에서 끝까지 읽는다. index 재사용/pooling이나 할당 최적화는 이번 측정에 넣지 않았다.

## 구현

- `incremental.go`: 기존 descriptor로 모든 알려진 최상위 필드 번호→slot 조회표를 한 번 만든다. 필터의 관심 필드 목록에 의존하지 않는다. 알 수 없는 번호는 건너뛴다.
- session은 원본 bytes, 현재 cursor, 필드별 offset, EOF/error를 가진다. 알려진 필드마다 raw value의 시작/끝 위치를 64bit 항목에 기록한다. 읽은 문자열은 원본 buffer를 참조한다.
- `first`: 요청 필드가 캐시에 있으면 반환, 없으면 cursor부터 진행하고 해당 필드를 찾으면 멈춘다. 검색 중 만난 다른 알려진 필드도 기록한다. 중복 scalar가 없다는 입력 가정을 요구한다.
- `last`: 최초 payload 필드 요청에서 끝까지 읽고 마지막 위치를 기록한다. 이후 필드 요청은 캐시에서 읽는다. payload 접근 없이 CEL이 끝나면 스캔하지 않는다.
- 첫 payload 접근 전에는 위치 배열도 할당하지 않는다. 없는 필드를 찾고 EOF에 도달하면 다음 missing 요청은 재스캔하지 않는다.
- 현재 Get/CEL 연결은 기존 실험과 같은 singular non-oneof 문자열 범위다. 다른 필드 위치도 기록하지만 repeated/map 값, message merge, oneof/presence/기본값의 범용 의미는 구현하지 않았다. dense table은 field4096, offset은32bit 제한이며 범용 API가 아니다.
- 문자열 UTF-8과 buffer 불변/수명은 입력 계약이다. `last`는 마지막 scalar 선택이며 전체 strict validation을 의미하지 않는다. 무관한 하위 메시지 내부를 검증하지 않고, first 모드는 읽지 않은 뒤쪽의 오류를 발견하지 못할 수 있다.

## 검증

`go test -tags 'wire,table,incremental' ./...` 통과. 별도 descriptor의 필드1/2/3으로 cursor를 확인해 `2→1→2→3→missing→missing`에서 재스캔이 없음을 검증했다. 2,000개 무작위 유효 메시지에 중복/빈 문자열/unknown field와 접근 순서 변경을 넣고, last는 dynamicpb, first는 첫 값 기준과 비교했다. 잘린 입력의 오류와 오류 발견 후 일관된 반환도 확인했다.

실제 CEL 단락 평가에서 `short_false`는 repo만 요청하고, `metadata_false`는 payload를 전혀 요청하지 않는 것을 검사했다. 모든 fixture/scenario의 direct·CEL 결과는 native CEL과 같았고 원격 실행 전 검사도 세 호스트에서 통과했다. 전체 protobuf conformance 검증은 아니다.

## 측정 조건

호스트 A/B/C, Ryzen5 7640HS, CPU2 고정/GOMAXPROCS1. Go1.27.1, 동일 binary SHA256,100ms×3회,순서 무작위. **4,410개 샘플 /490개 조건**. 호스트별 반복 중앙값의 중앙값을 사용했다. 호스트 범위는 JSON/HTML에 있으며 신뢰구간이 아니다. powersave governor를 바꾸거나 원격 패키지를 설치하지 않았다.

기존 cr1c81d49 Manifest와 payday5ac5cad schema,cel-go0.32.0,protobuf1.36.12. 이벤트마다 session/index 생성, decode와 CEL 평가를 포함한다. descriptor 준비와 CEL compile은 제외한다. SQL·권한검사·projection·전송은 제외하며 전체 Watch 처리량이 아니다.

이번 CEL 식은 접근 패턴을 분리하기 위해 `scenario.go`에 새로 정의했다. 이전 실험의 has/type/kind 조건이 붙은 Watch 식과 다르므로 이전 표의 절대 시간과 직접 비교하지 않는다. 각 비교는 이번 suite의 동일 식/입력/조건 안에서 한다.

비교군은 각 필드 접근 때 처음부터 읽는 `wire_*`, 관심 필드를 미리 받아 한 번 수집하는 `descriptor_table_*`, 새 `incremental_index_*`, 전체 decode1회인 `go_generated_unmarshal`이다. wire/table/incremental 비교는 같은 buffer alias/UTF-8 가정을 쓴다.

입력5개:small209B,map256약10KB,map256_late(artifact_type만 map뒤),map256_prefix(관심4필드를 모두 map뒤),blob1m(큰 annotation 하나). 논리적 필드1/2/3은 실제 Manifest repo#8/digest#9/media_type#10이다. getter 패턴과 같은 의미의 CEL 식을 함께 측정했다.

## 2→1→3 접근

관심 필드 앞에 map256개가 있는 입력, direct:

| 방식 | µs/event | Go B/op | alloc/op |
| --- | ---: | ---: | ---: |
| descriptor_table_first | 1.880 | 112 | 1 |
| descriptor_table_last | 1.923 | 112 | 1 |
| incremental_index_last | 2.031 | 192 | 2 |
| incremental_index_first | 2.103 | 192 | 2 |
| wire_first_trusted | 6.286 | 32 | 1 |
| wire_last_trusted | 6.477 | 32 | 1 |
| go_generated_unmarshal | 62.540 | 64493 | 1560 |

같은 접근을 CEL로 평가:

| 방식 | µs/event | Go B/op | alloc/op |
| --- | ---: | ---: | ---: |
| descriptor_table_last | 3.351 | 1304 | 23 |
| descriptor_table_first | 3.353 | 1304 | 23 |
| incremental_index_last | 3.511 | 1384 | 24 |
| incremental_index_first | 3.574 | 1384 | 24 |
| wire_first_trusted | 7.938 | 1224 | 23 |
| wire_last_trusted | 8.170 | 1224 | 23 |
| go_generated_unmarshal | 65.002 | 65689 | 1582 |

작은 입력에서 같은 direct 접근:

| 방식 | µs/event | Go B/op | alloc/op |
| --- | ---: | ---: | ---: |
| descriptor_table_first | 0.087 | 112 | 1 |
| wire_first_trusted | 0.096 | 32 | 1 |
| descriptor_table_last | 0.115 | 112 | 1 |
| incremental_index_first | 0.134 | 192 | 2 |
| incremental_index_last | 0.158 | 192 | 2 |
| wire_last_trusted | 0.218 | 32 | 1 |
| go_generated_unmarshal | 0.432 | 533 | 9 |

## 반복 접근과 누락

map256_prefix, 같은 필드를8번 읽기,direct:

| 방식 | µs/event | Go B/op | alloc/op |
| --- | ---: | ---: | ---: |
| descriptor_table_first | 1.943 | 112 | 1 |
| descriptor_table_last | 1.985 | 112 | 1 |
| incremental_index_last | 2.091 | 192 | 2 |
| incremental_index_first | 2.186 | 192 | 2 |
| wire_first_trusted | 16.655 | 32 | 1 |
| wire_last_trusted | 17.177 | 32 | 1 |
| go_generated_unmarshal | 62.935 | 64493 | 1560 |

map256, 없는 subject를4번 확인한 뒤 repo읽기,direct:

| 방식 | µs/event | Go B/op | alloc/op |
| --- | ---: | ---: | ---: |
| descriptor_table_first | 1.920 | 112 | 1 |
| descriptor_table_last | 1.945 | 112 | 1 |
| incremental_index_last | 2.035 | 192 | 2 |
| incremental_index_first | 2.118 | 192 | 2 |
| wire_first_trusted | 8.617 | 32 | 1 |
| wire_last_trusted | 11.014 | 32 | 1 |
| go_generated_unmarshal | 63.653 | 64493 | 1560 |

## CEL 단락 평가

repo에서 거부되어 뒤의 artifact_type은 요청하지 않는 경우,map256_late:

| 방식 | µs/event | Go B/op | alloc/op |
| --- | ---: | ---: | ---: |
| wire_first_trusted | 0.847 | 872 | 16 |
| incremental_index_first | 0.948 | 1032 | 17 |
| descriptor_table_last | 2.762 | 952 | 16 |
| descriptor_table_first | 2.765 | 952 | 16 |
| incremental_index_last | 2.903 | 1032 | 17 |
| wire_last_trusted | 3.152 | 872 | 16 |
| go_generated_unmarshal | 64.003 | 65336 | 1575 |

envelope의 kind에서 거부되어 payload 접근 자체가 없는 경우,map256:

| 방식 | µs/event | Go B/op | alloc/op |
| --- | ---: | ---: | ---: |
| wire_last_trusted | 0.722 | 760 | 14 |
| wire_first_trusted | 0.724 | 760 | 14 |
| incremental_index_first | 0.760 | 824 | 14 |
| incremental_index_last | 0.765 | 824 | 14 |
| descriptor_table_first | 0.800 | 840 | 14 |
| descriptor_table_last | 2.756 | 840 | 14 |
| go_generated_unmarshal | 62.727 | 65225 | 1573 |

metadata_false의 이득은 읽기를 첫 접근까지 미루는 효과다. 각 접근을 재검색하는 wire도 스캔을 생략하며, 기존 table도 초기화를 늦추면 같은 이득을 얻을 수 있다. 이를 점진적 offset 캐시만의 이득으로 해석하지 않는다.

## 모든 관심 필드를 읽는 경우

small,4필드,CEL:

| 방식 | µs/event | Go B/op | alloc/op |
| --- | ---: | ---: | ---: |
| descriptor_table_first | 1.800 | 1384 | 26 |
| descriptor_table_last | 1.826 | 1384 | 26 |
| incremental_index_first | 1.875 | 1464 | 27 |
| wire_first_trusted | 1.881 | 1304 | 26 |
| incremental_index_last | 1.941 | 1464 | 27 |
| wire_last_trusted | 2.053 | 1304 | 26 |
| go_generated_unmarshal | 2.305 | 1805 | 34 |

map256,4필드,CEL:

| 방식 | µs/event | Go B/op | alloc/op |
| --- | ---: | ---: | ---: |
| descriptor_table_first | 1.841 | 1384 | 26 |
| wire_first_trusted | 1.878 | 1304 | 26 |
| incremental_index_first | 1.901 | 1464 | 27 |
| descriptor_table_last | 3.629 | 1384 | 26 |
| incremental_index_last | 3.820 | 1464 | 27 |
| wire_last_trusted | 10.381 | 1304 | 26 |
| go_generated_unmarshal | 65.852 | 65768 | 1585 |

## 크기와 자료

stripped 동일 측정 프로그램: 기존 wire+table 구성 17,023,239B, 점진적 first/last 추가 17,031,431B, **증가 8KiB**. 실제 payday server 크기는 측정하지 않았다. session당 비용은 각 표의 B/op와 alloc/op에 포함된다.

- `report.html`: 모든 조건을 선택하고 정렬하는 오프라인 표.
- `results/summary.json`, `summary.csv`: 집계와 호스트별 중앙값.
- `results/scan-{A,B,C}.jsonl/.meta.json/.log`: 원시 결과, 실행 명령, hash, 종료 상태.
- [reader와 검사 코드](../../../../experiments/2026-10-15/incremental/).
- [재현 절차](../../../../experiments/2026-10-15/incremental/README.md)와 [reproduce.sh](../../../../experiments/2026-10-15/incremental/reproduce.sh).
- 입력 cr은 동일한 공개 커밋의 module version에 고정했다. 원래 로컬 replace와의 경로·module metadata 차이로 재빌드 hash/크기는 달라질 수 있다.

소스와 결과는 이 레포의 experiments/ 및 docs/experiments/에 보존한다.
