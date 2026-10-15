# CEL / protobuf wire 읽기 실험 결과

2026-10-15. 호스트 A·B·C에서 요청한 비교를 완료했다. 결론은 **이미 해석된 메시지는 재사용하고, bytes만 있는 경로는 Go에서 기존 descriptor로 접근 계획을 준비하는 것**을 우선하는 것이다. 이 실험에서는 런타임 조회 테이블이 빌드타임 코드 생성과 사실상 같은 CEL 성능을 냈다. C는 많은 tag를 끝까지 훑는 경우에 추가 이점이 있었지만, 작은 메시지와 조기 종료에서는 언어를 바꾸는 효과가 작았다. Rust 바인딩의 우위는 관찰되지 않았다.

사용자는 바이너리 증가량의 고정 상한을 두지 않고 성능과 함께 판단하기로 했다. 아래 증가량은 동일한 stripped 측정 프로그램에 각 구현군을 추가한 차이이며, 실제 payday 서버의 증가량을 측정한 것은 아니다.

## 조건과 해석

- 세 호스트: A, B, C, 모두 AMD Ryzen 5 7640HS, 12 threads, 약 4.4GB RAM. Linux 7.0, glibc 2.43. powersave governor를 변경하지 않았다.
- 로컬 Go 1.27.1, GCC, Rust 1.99.0으로 동일한 Linux amd64 바이너리를 빌드해 배포했다. 원격에 컴파일러/패키지를 설치하지 않았다. 각 바이너리 SHA256은 세 호스트에서 일치한다.
- 각 호스트 CPU 2에 고정, GOMAXPROCS=1, 한 호스트에서 한 측정만 실행. case당 100ms, 기본 3회, FFI annotation 비교는 5회. 순서는 호스트와 반복마다 섞었다.
- 총 **14,526개 측정 샘플**, **1,526개 조건 집계**. 호스트별 반복 중앙값을 구한 뒤 세 호스트 중앙값의 중앙값을 사용했다. HTML/JSON에는 호스트 간 범위도 있다. 이 범위는 신뢰구간이 아니다.
- 실제 cr Manifest와 payday WatchEvent를 사용했다. cr는 `1c81d493a8498e3ccefe2ba6f9d64c61fbe7dcd3`, payday는 `5ac5cad674897ebc3f652f756fe4da363dc72de0`.
- descriptor 인스턴스, 관심 필드 번호, CEL 프로그램, 각 라이브러리 접근 계획은 측정 전에 준비했다. `prepare_ns`는 별도 기록했다. 이벤트마다 필요한 session/decode, CEL activation, cost tracking은 측정에 포함했다.
- SQL 권한검사, projector의 clone/unpack/repack, broker, gRPC, 네트워크, 최초 CEL 컴파일은 제외했다. 전체 Watch 처리량으로 해석할 수 없다.
- `decoded_reuse`는 입력이 이미 해석돼 있는 상황의 기준선이며 초기 decode 비용이 없다. 다른 decoder의 전체 비용과 동일한 시작 조건이 아니다.
- query1은 `type + CREATED + artifact_type`, query4는 여기에 repo/digest prefix/media_type 조건을 더한다. full CEL 식은 lab.go에 있다. 관심 필드 계획은 실험이 명시적으로 제공하며, 임의 CEL AST에서 접근 경로를 자동 추출하는 기능은 구현하지 않았다.

| 입력 | 내용 |
| --- | --- |
| small | Manifest 209B |
| blob8k | 8KiB annotation 하나, 전체 8430B |
| map256 | 짧은 annotation 256개, 전체 9937B; artifact_type 뒤에 259개 tag |
| blob1m | 1MiB annotation 하나 |
| map256_late | map256의 artifact_type을 마지막으로 이동 |
| missing | artifact_type이 없는 메시지 |

## 기본 비교: CEL 전체 비용

단위 µs/event. 아래는 main suite에서 같은 조건으로 비교한 결과다. `first`는 scalar 중복이 없다는 입력 계약 아래 첫 일치에서 끝내며, `last`는 뒤의 scalar 출현을 고려한다. 여러 필드를 미리 읽는 경로는 모든 관심 필드를 찾으면 종료한다.

| 방식 | small, 1필드 | small, 4필드 | map256, 1필드 | blob1m, 1필드 |
| --- | ---: | ---: | ---: | ---: |
| native_cel_any | 4.042 | 8.384 | 145.466 | 676.565 |
| decoded_reuse | 1.643 | 2.556 | 1.679 | 1.637 |
| go_generated_unmarshal | 2.251 | 3.156 | 70.284 | 166.781 |
| go_dynamicpb | 3.830 | 4.788 | 79.172 | 168.836 |
| hyperpb_copy | 2.436 | 3.391 | 24.956 | 215.728 |
| hyperpb_alias | 2.332 | 3.255 | 22.656 | 31.610 |
| dynamicgo_path | 1.857 | 3.164 | 1.856 | 1.844 |
| csproto_safe | 1.880 | 3.010 | 5.432 | 131.317 |
| csproto_fast | 1.805 | 2.739 | 3.687 | 1.834 |
| molecule_last | 1.761 | 2.875 | 3.746 | 1.774 |
| molecule_first | 1.727 | 2.675 | 1.712 | 1.743 |
| wire_last_copy | 1.805 | 3.034 | 3.737 | 1.812 |
| wire_last_alias | 1.762 | 2.937 | 3.830 | 1.768 |
| wire_first_alias | 1.730 | 2.771 | 1.730 | 1.743 |
| wire_first_trusted | 1.713 | 2.717 | 1.727 | 1.744 |
| plan_last_alias | 1.819 | 2.781 | 4.443 | 1.833 |
| plan_first_alias | 1.846 | 2.750 | 1.779 | 1.786 |
| go_codegen_last | 1.756 | 2.674 | 3.551 | 1.782 |
| go_codegen_first | 1.749 | 2.643 | 1.742 | 1.765 |
| c_generic_last | 1.768 | 2.918 | 2.363 | 1.765 |
| c_generic_first | 1.733 | 2.780 | 1.729 | 1.732 |
| c_codegen_last | 1.821 | 2.721 | 2.434 | 1.797 |
| c_codegen_first | 1.805 | 2.730 | 1.787 | 1.823 |
| rust_generic_last | 1.800 | 3.078 | 3.027 | 1.820 |
| rust_generic_first | 1.781 | 2.953 | 1.776 | 1.790 |
| rust_codegen_last | 1.821 | 2.799 | 3.074 | 1.851 |
| rust_codegen_first | 1.824 | 2.742 | 1.811 | 1.812 |

`native_cel_any`는 현행 기본 cel-go protobuf adapter이며 반복된 Any 접근에 unpack 비용이 들어간다. `go_generated_unmarshal`은 이벤트마다 한 번 decode하고 그 결과를 같은 CEL 평가에서 재사용하며, 이 decode 비용도 측정에 포함한다. 같은 평가 안에서 반복 unpack을 피하는 것만으로도 small은 약 4.04→2.25µs로 줄었다. selective 경로는 약 1.7–1.9µs로 모여, 이 구간에서는 CEL interpreter/activation 비용의 비중이 크다.

큰 bytes 값 하나는 길이를 읽고 건너뛸 수 있다. 반면 map 항목들은 각각 tag가 있어 끝까지 확인할 때 비용이 늘어난다. 높은 배수는 읽지 않는 데이터가 많은 실험 입력의 효과이며 모든 메시지에 적용되는 가속 비율이 아니다.

## 기존 descriptor 활용과 코드 생성

등록된 descriptor에서 필드 번호→결과 slot 배열을 한 번 만든 경로를 추가했다. 비교 대상들은 같은 별도 table suite에서 실행했다. `kernel`은 C/Rust와 같은 좁은 wire type 집합(0,1,2,5)을 다루는 단순한 Go 스캔을 사용한다. `go_codegen`은 protowire 기반 생성 코드, `go_kernel_project`는 단순 스캔과 생성된 필드 번호 switch를 사용한다.

| 방식 | small direct 1필드 ns | small CEL 4필드 µs | map256 CEL 1필드 µs | map256_late direct 4필드 ns |
| --- | ---: | ---: | ---: | ---: |
| descriptor_table_last | 99.3 | 2.650 | 3.545 | 1867.1 |
| descriptor_table_first | 79.1 | 2.625 | 1.760 | 1864.2 |
| descriptor_table_last_kernel | 84.6 | 2.637 | 3.231 | 1569.1 |
| descriptor_table_first_kernel | 68.3 | 2.616 | 1.734 | 1566.0 |
| go_codegen_last | 107.3 | 2.655 | 3.642 | 1976.2 |
| go_codegen_first | 86.2 | 2.642 | 1.787 | 1971.7 |
| go_kernel_project_last | 95.4 | 2.651 | 3.277 | 1622.6 |
| go_kernel_project_first | 78.4 | 2.646 | 1.746 | 1625.7 |
| c_codegen_last | 98.0 | 2.679 | 2.347 | 668.4 |
| c_codegen_first | 93.8 | 2.678 | 1.783 | 671.4 |

코드 생성이 런타임 descriptor 조회 테이블보다 확실히 빠르다는 결과는 없었다. 따라서 모든 리소스마다 parser 코드를 생성해야 한다는 근거는 부족하다. descriptor를 이용해 numeric dispatch와 접근 계획을 준비하는 경로부터 시작할 수 있다. 이 실험의 dense table은 최대 필드 번호 4096까지로 제한했다. 범용 구현은 큰 field number에 sparse dispatch를 제공해야 한다.

kernel suite에서는 단일 필드의 작은 입력을 Go 단순 스캔으로 읽는 데 약 45.5ns, C generic에 약 52.6ns가 들었다. 많은 tag를 끝까지 훑는 경우에는 C의 이점이 남았다. map256_late 4필드의 단일 스캔은 C 약 0.67µs, Go 단순 projection 약 1.62µs였다. 이를 CEL까지 포함하면 parser 속도 비율보다 차이가 작아진다.

## C/Rust 호출, batch, 재사용

C/Rust에 전달되는 buffer는 호출 동안만 사용하고 보관하거나 Go callback을 호출하지 않는다. 이 계약을 확인한 후 `#cgo noescape`와 `#cgo nocallback`을 켠 별도 바이너리도 측정했다. 기존과 annotation 적용 버전은 각각 main/ffiopt suite로 보존했다. 최적화 적용 후 빈 C/Rust 호출은 각각 약 23ns였다.

아래 batch는 네 문자열을 추출하고 동일한 조건으로 비교하는 **CEL 없는 별도 실험**이다. 입력을 연속 buffer에 모으는 비용과 output scratch allocation은 측정 전에 준비했다. 실시간 이벤트를 모으기 위한 지연이나 packing 비용을 포함하지 않는다. 조기 종료, small 입력 기준이다.

| 방식 | batch 1 ns/message | batch 32 ns/message | Go alloc/op |
| --- | ---: | ---: | ---: |
| go_first | 44.0 | 43.0 | 0 |
| c_first | 49.5 | 25.3 | 0 |
| rust_first | 72.8 | 46.9 | 0 |

작은 입력에서는 단일 C 호출보다 Go가 빠르고, batch로 묶으면 C의 내부 처리 이점을 더 얻는다. Rust는 이번 코드/컴파일 옵션에서 C나 Go보다 나은 결과가 없었다. 이것은 언어 일반의 우열이 아니라 여기서 작성한 kernel의 측정이다.

32개 구독자에게 map256 이벤트를 전달하는 모델도 측정했다. query4, 단위는 **한 이벤트를 32번 평가하는 총 µs**다. subscriber별 authorization/projection은 포함하지 않는다. decode 공유는 같은 읽기 이미지가 이미 허용됐다는 가정의 비용 모델이며, 권한검사를 공유하거나 생략하라는 뜻이 아니다.

| 경로 | frame 재사용 | decode 공유 | 총 µs / 32평가 | Go B/op |
| --- | --- | --- | ---: | ---: |
| go_generated_unmarshal | False | False | 2315.28 | 2109690 |
| go_generated_unmarshal | True | False | 2265.33 | 2097914 |
| go_generated_unmarshal | True | True | 149.97 | 98546 |
| go_codegen_first | False | False | 85.30 | 49410 |
| go_codegen_first | True | True | 79.86 | 34161 |

full decode를 구독자마다 반복하는 비용이 크므로, 프레임워크의 이미지 재사용과 라우팅도 중요한 최적화다. CEL Frame을 구독자별로 재사용하면 input map/wrapper 할당을 줄일 수 있다. Frame은 한 goroutine이 소유하며 구독자 간에 공유하지 않는다.

## 바이너리 크기

모두 `go build -trimpath -ldflags '-s -w'`로 같은 cmd/bench를 빌드했다. baseline은 cr의 기존 schema와 CEL/기본 protobuf 경로를 포함한다. 각 구현군의 상대 크기이며, 실행 파일만으로는 알 수 없는 공유 라이브러리 의존성이 있는지도 별도 ldd 결과로 남겼다. C/Rust reader 자체는 정적으로 링크된다. Go alloc 지표는 Go heap 지표이고 외부 allocator의 일반적인 비용을 나타내지 않는다. 이번 custom C/Rust kernel은 heap allocation을 하지 않는다.

| 추가 구현군 | 실행 파일 bytes | baseline 대비 KiB |
| --- | ---: | ---: |
| baseline | 17,006,855 | 0.0 |
| wire | 17,019,143 | 12.0 |
| gencode | 17,010,951 | 4.0 |
| csproto | 17,928,455 | 900.0 |
| hyperpb | 21,209,351 | 4104.0 |
| molecule | 17,019,143 | 12.0 |
| dynamicgo | 19,067,143 | 2012.0 |
| cffi | 17,023,512 | 16.3 |
| rustffi | 17,023,672 | 16.4 |
| kernel | 17,019,143 | 12.0 |
| table | 17,019,143 | 12.0 |

기존 라이브러리는 Molecule 약 12KiB, csproto 약 0.88MiB, DynamicGo 약 1.96MiB, HyperPB 약 4.01MiB가 추가됐다. 자체 Go/C/Rust 실험 경로는 약 4–17KiB였다. **자체 경로는 네 문자열의 제한된 projection이며 범용 protobuf 구현과 기능 범위가 다르다.** Rust는 no_std로 빌드했으므로 일반적인 Rust std/Prost 런타임 크기를 의미하지 않는다. generated code의 리소스 수별 증가량은 측정하지 않았다.

## 구현 계약과 검증 범위

실제 CEL 식과 읽은 값은 여섯 fixture에서 기본 protobuf 결과와 비교했다. 누락과 재정렬을 포함하고, duplicate scalar는 각 경로의 first/last 계약에 맞게 확인했다. C/Rust 경계에는 5,000개 잘린/임의 byte 입력과 1,000개 유효한 반복 필드 입력을 추가로 검사했다. 각 원격 실행 전 결과 일치 검사도 통과했다. 이 검증은 전체 protobuf conformance test가 아니다.

`last`는 duplicate scalar 의미를 가리키며 전체 strict validation을 뜻하지 않는다. 무관한 하위 메시지 내부는 검사하지 않을 수 있고, C/Rust 및 일부 Go kernel은 UTF-8 유효성·입력 불변을 가정하며 group을 지원하지 않는다. oneof, message merging, list/map 값 접근, WKT, message equality를 CEL adapter에서 일반화하지 않았다. 타입 확인과 선택된 문자열 접근의 비용을 연구하는 POC다.

기존 구현별로도 차이가 있다. DynamicGo는 자체 descriptor가 필요해 기존 필드 정보를 작은 projection schema로 변환한 뒤 초기화 시 한 번 파싱했다. lazyproto는 관심 raw field를 모은 뒤 값을 해석하고, Fast는 복사 생략/수명 계약을 뜻한다. HyperPB는 표준 descriptor를 받아 parser를 만들지만 여기서는 전체 message를 decode한다. Molecule은 저수준 streaming API다. pinned source: DynamicGo v0.9.2, csproto v0.37.0, HyperPB v0.1.3, Molecule v1.0.0, cel-go v0.32.0, protobuf v1.36.12.

DynamicGo 의존성 Sonic은 Go1.27에서 JSON AST fallback 경고를 출력했다. 이 실험은 protobuf 경로를 사용하며 JSON benchmark가 아니다. 경고는 원시 로그에 보존했다. 버전 제약과 지원 범위는 제품 채택 시 검토 대상이다.

## 판단과 후속 구현 범위

1. payday projector에서 이미 unmarshal된 공개 이미지를 CEL에 재사용하는 경로를 먼저 고려한다. 현재 pipeline 앞단 비용까지 wire adapter 하나가 없애지는 않는다.
2. bytes만 있는 경로는 기존 descriptor를 이용한 Go 접근 계획/조회 테이블로 시작한다. 이 측정은 코드 생성을 반드시 추가할 필요가 있다는 근거를 제공하지 않는다.
3. 기존 라이브러리를 재사용하려면 csproto/lazyproto Fast가 간단한 후보이며, 불변 buffer와 결과 수명 계약을 맞춰야 한다. Molecule은 작지만 더 많은 protobuf 의미를 호출자가 담당한다. DynamicGo/HyperPB의 기능과 크기는 실제 필요한 기능 범위에 맞춰 판단한다.
4. scalar 중복 없음이 보장되는 경로의 early return은 옵션으로 가치가 있다. 외부에서 온 모든 유효한 protobuf가 이 조건을 만족하지는 않는다.
5. C는 많은 tag를 스캔해야 하는 부하나 이미 모인 batch에서 추가 후보로 남긴다. 이번 결과만으로 일반 Watch에 C/Rust 의존성을 추가할 필요는 보이지 않는다.

제품 코드/PR의 구현은 이 실험과 별개다. 일반화에 앞서 arbitrary CEL 접근 경로, authorization/public projection, 메시지 소유권과 protobuf 의미를 포함한 API를 정해야 한다.

## 자료와 재현

- `report.html`: 입력·단계·필드 수·fanout 조건을 고르고 정렬할 수 있는 오프라인 표.
- `results/summary.csv`, `summary.json`: 모든 집계와 호스트별 중앙값.
- `results/{main,ffiopt,extra,kernel,table}-{A,B,C}.jsonl`: 14,526개 원시 샘플.
- 대응 `.meta.json`: 실행 명령, binary SHA256, 시간, 최대 RSS, 종료 코드.
- `results/size_summary.json`, `buildinfo.txt`, `inventory-*.json`: 크기와 환경.
- [측정 소스](../../../../experiments/2026-10-15/alternatives/): descriptor 기반 Go/C/Rust dispatch와 CEL adapter.
- [재현 절차](../../../../experiments/2026-10-15/alternatives/README.md)와 [reproduce.sh](../../../../experiments/2026-10-15/alternatives/reproduce.sh).
- [최초 main 소스](../../../../experiments/2026-10-15/alternatives/main-source/): 후속 FFI annotation, Frame, kernel, table 변경 전 snapshot.
- 입력 cr은 [1c81d493a849](https://github.com/lesomnus/cr/tree/1c81d493a8498e3ccefe2ba6f9d64c61fbe7dcd3)에 고정했다. 공개 소스의 go.mod는 동일 커밋의 module version을 사용한다. 원래 로컬 replace와의 경로·module metadata 차이로 재빌드 hash/크기는 달라질 수 있다.

소스와 결과는 이 레포의 experiments/ 및 docs/experiments/에 보존한다. 이 측정 당시 제품 저장소를 변경하거나 배포하지 않았다.
