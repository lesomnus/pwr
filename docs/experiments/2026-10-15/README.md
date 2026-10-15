# 2026-10-15 CEL / protobuf wire 실험

기존 구현과 최적화 경로를 비교한 뒤, 요청한 필드까지 진행하며 지나친 위치를
기억하는 점진적 reader를 구현해 추가 측정했다. 세 테스트 머신에서 총
**18,936개 원시 샘플, 2,016개 집계 조건**을 기록했다.

공개 API 확장 후의 별도 로컬 측정은 [public-api](public-api/README.md)에 있다.
위 샘플 수와 아래 historical PoC 결과에는 포함하지 않는다.

| 실험 | 보고서 | 원시 결과·집계 | 측정 코드 |
| --- | --- | --- | --- |
| Go·기존 라이브러리·Go/C/Rust 생성·바인딩 | [전체 비교](alternatives/REPORT.ko.md) | [results](alternatives/results/) | [standalone module](../../../experiments/2026-10-15/alternatives/) |
| 재검색·사전 수집·점진적 index | [점진적 스캔](incremental/REPORT.ko.md) | [results](incremental/results/) | [standalone module](../../../experiments/2026-10-15/incremental/) |

[전체 비교 HTML](alternatives/report.html)과 [점진적 스캔 HTML](incremental/report.html)은
조건을 선택하고 정렬할 수 있는 오프라인 표다. 레포를 내려받아 브라우저에서
열면 된다. GitHub의 파일 보기에서는 JavaScript 표가 실행되지 않는다.

## 주요 결과

첫 실험의 작은 입력·한 필드 CEL 조건은 기본 Any 경로 4.042µs, 이벤트당
Go 전체 decode 한 번 2.251µs, 선택 읽기 약 1.7–1.9µs였다. 이미 decode한 값을
재사용하는 기준은 1.643µs지만 최초 decode 비용을 제외한 수치다.

숫자 기반 descriptor table과 생성된 Go switch는 작은 네 필드 CEL 조건에서
약 2.62–2.65µs로 비슷했다. C는 많은 tag를 스캔하거나 batch로 처리하는 일부
조건에서 이점이 있었고, 작성한 Rust kernel은 C/Go보다 나은 결과가 없었다.
이는 각 kernel의 결과이며 언어 전체의 성능 비교가 아니다.

점진적 실험의 대표 조건은 다음과 같다. 단위는 µs/event, first 모드이며
singular scalar가 중복되지 않는 입력을 가정한다.

| 조건 | 매번 재검색 | 관심 필드 사전 수집 | 점진적 index |
| --- | ---: | ---: | ---: |
| 앞에 map256, 2→1→3 직접 접근 | 6.286 | 1.880 | 2.103 |
| 앞에 map256, 같은 필드 8회 | 16.655 | 1.943 | 2.186 |
| CEL 앞 조건 false로 뒤쪽 필드 생략 | 0.847 | 2.765 | 0.948 |
| 작은 입력의 네 필드 CEL | 보고서 참조 | 1.800 | 1.875 |

점진적 index는 반복 검색을 없애고 뒤쪽 분기를 생략할 수 있다. 필요한 필드를
모두 미리 알고 읽는 경우에는 사전 수집이 조금 더 빠르다. 세션 생성 비용은
사전 수집 112B/1alloc, 점진적 192B/2alloc이었다. 같은 stripped 측정 프로그램에
점진적 first/last를 추가했을 때 바이너리는 8KiB 증가했다.

첫 실험의 추가 바이너리 크기는 Go codegen 4KiB, descriptor/kernel/Molecule
12KiB, C 약 16.3KiB, Rust no_std 약 16.4KiB, csproto 900KiB, DynamicGo 2012KiB,
HyperPB 4104KiB였다. 자체 kernel은 네 문자열만 읽으므로 범용 라이브러리와
기능 범위가 같지 않다. 실제 payday 서버나 다수 schema의 크기 증가는 측정하지 않았다.

## 측정 방법과 해석 범위

- 세 호스트 A/B/C: Ryzen 5 7640HS, 12 threads, 약 4.4GB RAM, Linux 7,
  glibc 2.43, powersave. CPU 2 고정, GOMAXPROCS=1.
- Go 1.27.1, Rust 1.99.0. 실제 버전·명령·binary SHA256·RSS·종료 코드는
  각 `results/`의 inventory, buildinfo, meta JSON과 로그에 보존했다.
- 조건당 100ms×3회, FFI 비교만 5회. 실행 순서를 섞고 호스트별 중앙값을
  구한 뒤 세 중앙값의 중앙값을 집계했다. 호스트 범위는 신뢰구간이 아니다.
- 전체 비교는 14,526 samples / 1,526 conditions, 점진적 비교는
  4,410 samples / 490 conditions. 모든 원격 측정은 exit 0이었다.
- descriptor/plan/CEL compile은 사전 준비했다. 이벤트당 session/decode 비용은
  포함하며, `decoded_reuse`는 별도로 최초 decode를 제외한다.
- SQL·권한 검사·projection·broker·네트워크는 제외했다. 전체 Watch 성능으로
  해석할 수 없다. 두 실험의 CEL 식이 달라 suite 사이 절대시간 직접 비교도 피한다.
- `last`는 마지막 scalar 선택을 뜻하며 full strict validation이 아니다.
  nested merge, oneof, list/map 값, WKT/Any, equality를 일반화한 실험이 아니다.

## 소스와 재현

루트 pwr API는 실험 이후 UTF-8 기본 검증, sparse 필드 번호와 descriptor
기본값 처리를 보완했다. **이 측정값은 현재 공개 패키지의 성능 보장이 아니다.**
Historical module은 독립된 go.mod를 사용하며 루트 패키지에 연결되지 않는다.

각 소스 디렉터리에서 `sh reproduce.sh`로 빌드와 값 일치 검사를 실행할 수
있다. 전체 비교에는 C compiler와 Rust가 필요하며 점진적 비교에는 Go만 필요하다.
최초 main 비교 소스는 `alternatives/main-source/`에 별도로 남겼다. 원시 metadata의
명령은 당시 바이너리 이름을 사용하므로 재현 스크립트의 출력 이름에 맞춰 실행한다.

입력 cr은 공개 커밋 `1c81d493a8498e3ccefe2ba6f9d64c61fbe7dcd3`에 고정했다.
측정 당시의 로컬 replace를 같은 커밋의 공개 module version으로 바꿨으므로
재빌드 hash와 크기는 module metadata 차이로 달라질 수 있다.

집계 재생성은 각 보고서 디렉터리에서 실행한다:

```sh
python3 analyze.py
python3 write_report.py
```

루트의 `python3 scripts/check_experiments.py`는 원시 샘플 개수·반복 횟수·
호스트별 바이너리 일치·집계값·생성된 보고서의 재현성을 확인한다.
[provenance.json](provenance.json)에 원본 압축본 해시와 공개용 변경을 기록했다.
호스트 식별자는 A/B/C로 치환했고 측정 숫자는 변경하지 않았다. 실행 파일,
Rust 정적 라이브러리, 중복 cr 소스 압축본은 포함하지 않았다.
