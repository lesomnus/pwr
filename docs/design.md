# pwr 설계 결정

상태: 초기 experimental API. 아래의 현재 구현과 후속 설계를 구분한다.

## 목적과 현재 범위

이미 런타임에 올라온 protobuf descriptor를 재사용하여 wire bytes에서 필요한
필드만 읽는다. 메시지 전체를 객체로 만들지 않고, 접근 시 tag를 탐색하고
위치를 캐시한다. CEL이나 payday를 모르는 독립 Go 패키지로 유지한다.

현재 모든 singular scalar getter, nested message/group 병합, real oneof와
presence를 지원한다. repeated/map, CEL adapter와 재사용 기능은 다음 구현 단위다.
`WithLimits`로 중첩 깊이와 reader tree의 wire record/byte 방문량을 제한한다.


## 확정한 기본 구조

`Compile(descriptor)`는 필드 번호→slot 조회 구조와 문자열 기본값을 한 번
준비한다. 메시지별 reflection lookup이나 .proto 파싱은 하지 않는다.
최대 필드 번호가 4096 이하면 dense table, 그보다 크면 sparse map을 사용한다.
`Schema`는 불변이며 여러 goroutine에서 공유할 수 있다.

`Schema.NewReader(raw, options...)`는 메시지 하나의 cursor, offset index,
EOF와 오류 상태를 가진다. 복합 필드는 최초~최종 occurrence의 범위를 저장하고,
실제 요청할 때 그 범위를 다시 읽어 필요한 payload 구간을 모은다. 무관한
repeated/map occurrence마다 메모리를 할당하지 않는다. 입력은 복사하지 않으며 index 배열도 첫 지원 필드
접근까지 할당하지 않는다. Reader는 동시 사용하지 않는다. offset은 32bit로
저장하므로 입력은 2 GiB 미만으로 제한한다.

현재는 별도 코드 생성기나 C/Rust 의존성을 추가하지 않는다. 실험의 숫자 기반
descriptor table은 생성된 Go switch와 비슷한 CEL 시간을 보였고, C의 이점은
많은 tag를 읽거나 batch 처리하는 제한된 조건에서 컸다. 패키지별 자세한
범위와 수치는 [실험 기록](experiments/2026-10-15/README.md)에 있다.

## 중복 scalar와 점진적 스캔

기본값은 마지막 scalar occurrence를 선택한다. 따라서 첫 payload 접근에서
현재 메시지 끝까지 스캔하고 이후에는 캐시를 쓴다. 미래의 중복을 배제할 수
없는 일반 입력에서는 첫 일치만 보고 최종값을 반환할 수 없다.

`AssumeUniqueScalars()`를 명시하면 호출자가 singular scalar가 중복되지
않음을 보장한다. Reader는 요청 필드를 만나는 즉시 멈추며, 지나친 알려진
필드도 index에 기록한다. 예를 들어 wire 순서가 1, 2, 3이면:

1. 2 요청: 1과 2를 스캔하고 위치를 캐시한다.
2. 1 요청: 캐시를 사용한다.
3. 3 요청: 2 다음의 cursor부터 이어 읽는다.
4. 없는 필드 요청: EOF까지 읽고, 이후 누락 요청은 다시 스캔하지 않는다.

번호 정렬은 가정하지 않는다. 한 unknown length-delimited 필드의 payload는
길이만 확인하고 건너뛰므로, 스캔은 payload의 모든 byte를 해석한다는 뜻이
아니다. 다만 요청 필드에 도달하기까지 앞선 tag와 길이는 읽어야 한다.

관심 필드를 미리 모두 아는 경우에는 한 번에 수집하는 방식이 더 저렴할 수
있다. 점진적 index는 실행 중 접근 필드가 정해지거나 CEL 단락 평가처럼 뒤의
접근을 생략할 수 있을 때 유용하다. 정적 계획 수집 API는 현재 제공하지 않는다.

## 검증, 오류와 소유권

- 반환 문자열은 기본적으로 UTF-8을 검증한다. `AssumeValidUTF8()`는 이 검사만
  생략하며 tag/길이 경계 검사는 유지한다.
- 검증은 선택적이다. 건너뛴 nested body는 검사하지 않는다. 기본 last 모드도
  full protobuf validator가 아니며, 덮어써진 이전 문자열의 UTF-8 오류를 모두
  찾는다는 계약은 없다. unique 모드에서는 아직 읽지 않은 뒷부분 오류도
  발견하지 않을 수 있다.
- 잘못된 wire type으로 나타난 문자열 필드는 unknown record처럼 건너뛴다.
  올바른 문자열 occurrence를 덮어쓰지 않는다.
- 누락 필드는 descriptor default를 반환한다. `Has`는 explicit presence 또는
  implicit scalar의 non-default 여부를 따른다. `WhichOneof`는 최종 선택을 반환한다.
  required 필드의 존재를 일반 getter가 강제로 검증하지는 않는다.
- 발견한 wire/UTF-8 오류는 sticky다. unknown/unsupported getter 요청은
  Reader의 나머지 정상 접근을 막지 않는다. nil/zero Schema와 과대 입력은
  첫 `String` 호출에서 오류로 보고한다.
- 반환 문자열은 `unsafe.String`을 통해 원본 bytes를 참조한다. **Reader 또는
  반환 문자열을 쓰는 동안 buffer를 변경·재활용하지 않는다.** Reader를 버린
  뒤에도 반환 문자열을 사용한다면 이 계약은 계속된다. 독립 수명이 필요하면
  호출자가 문자열을 복사한다.

## Nested message와 oneof

`Message(number)`는 child Reader 또는 미존재 시 nil을 반환한다. descriptor
그래프는 memoization으로 자기참조를 처리하고, child는 payload 구간들을 참조해
복사 없이 읽는다. 중복 singular message/group의 구간을 wire 순서로 읽어 병합한다.
여러 occurrence를 합친 child에서는 scalar uniqueness 옵션과 무관하게 last를 택한다.

Oneof는 최종 선택을 확인하기 위해 부모 스캔을 끝낸다. 같은 message 멤버의
연속 occurrence는 병합하지만 다른 멤버로 전환하면 이전 내용은 폐기한다.
번호 순서는 가정하지 않는다. [공식 wire 의미](https://protobuf.dev/programming-guides/encoding/#last-one-wins)와
표준 Go decoder에 대한 differential test로 확인한다.

기본 최대 중첩은 100이며 unknown group도 명시적 stack으로 제한한다.
MaxFields/MaxBytes는 설정 시 child와 공유하며, 복합 필드 추출을 위한 재방문도 센다.
일반 message body는 getter에서 접근할 때까지 내부를 해석하지 않는다.

## 실험 수치의 적용 범위

2026-10-15 측정은 `experiments/`의 historical PoC를 대상으로 했다. 공개 패키지는
그 뒤 sparse 번호 처리, descriptor 기본값, UTF-8 기본 검증 등을 보완했으므로
측정값을 현재 라이브러리의 성능 보장으로 제시하지 않는다.

CEL adapter는 별도 계층으로 구성할 수 있다. 실제 payday 적용에서는 이미
unmarshal된 공개 이미지를 재사용하는 경로도 검토해야 한다. wire reader만으로
권한 검사·projection·SQL·broker·전송 비용이 사라지지는 않는다.
