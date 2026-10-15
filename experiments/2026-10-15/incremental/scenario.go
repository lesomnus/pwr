package lab

import (
	"fmt"
	"strings"

	"github.com/lesomnus/payday/pdpb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type ScanScenario struct {
	Name   string
	Fields []protoreflect.FieldDescriptor
	Query  string
	Direct func(*pdpb.WatchEvent, Session) (bool, error)
}

func ScanScenarios() []ScanScenario {
	repo, digest, media, artifact, subject := Fields[1], Fields[2], Fields[3], Fields[0], Descriptor.Fields().ByName("subject")
	var out []ScanScenario
	add := func(name string, order []protoreflect.FieldDescriptor) {
		var unique []protoreflect.FieldDescriptor
		seen := map[protoreflect.FieldNumber]bool{}
		var terms []string
		var wanted []string
		for _, fd := range order {
			if !seen[fd.Number()] {
				unique = append(unique, fd)
				seen[fd.Number()] = true
			}
			terms = append(terms, fmt.Sprintf("event.value.%s == %q", fd.Name(), expectedString(fd)))
			wanted = append(wanted, expectedString(fd))
		}
		out = append(out, ScanScenario{name, unique, strings.Join(terms, " && "), func(_ *pdpb.WatchEvent, s Session) (bool, error) {
			for i, fd := range order {
				v, e := s.Get(fd)
				if e != nil || v != wanted[i] {
					return false, e
				}
			}
			return true, nil
		}})
	}
	add("single_2", []protoreflect.FieldDescriptor{digest})
	add("sequence_2_1_3", []protoreflect.FieldDescriptor{digest, repo, media})
	add("repeat_2_x8", []protoreflect.FieldDescriptor{digest, digest, digest, digest, digest, digest, digest, digest})
	add("all4", []protoreflect.FieldDescriptor{artifact, repo, digest, media})
	add("missing_x4_then_1", []protoreflect.FieldDescriptor{subject, subject, subject, subject, repo})
	out = append(out, ScanScenario{"short_false", []protoreflect.FieldDescriptor{repo, artifact}, `event.value.repo == "reject" && event.value.artifact_type == "` + Artifact + `"`, func(_ *pdpb.WatchEvent, s Session) (bool, error) {
		v, e := s.Get(repo)
		if e != nil || v != "reject" {
			return false, e
		}
		v, e = s.Get(artifact)
		return v == Artifact, e
	}})
	out = append(out, ScanScenario{"metadata_false", []protoreflect.FieldDescriptor{artifact}, `event.kind == 2 && event.value.artifact_type == "` + Artifact + `"`, func(e *pdpb.WatchEvent, s Session) (bool, error) {
		if e.GetKind() != pdpb.ChangeKind_CHANGE_KIND_UPDATED {
			return false, nil
		}
		v, err := s.Get(artifact)
		return v == Artifact, err
	}})
	return out
}
func expectedString(fd protoreflect.FieldDescriptor) string {
	switch fd.Name() {
	case "repo":
		return "stage"
	case "digest":
		return "sha256:" + strings.Repeat("a", 64)
	case "media_type":
		return "application/vnd.oci.image.manifest.v1+json"
	case "artifact_type":
		return Artifact
	case "subject":
		return ""
	}
	panic("unexpected field")
}

func ScanFixtures() []Fixture {
	var out []Fixture
	for _, f := range Fixtures() {
		if f.Name != "small" && f.Name != "map256" && f.Name != "map256_late" && f.Name != "blob1m" {
			continue
		}
		out = append(out, f)
		if f.Name != "map256" {
			continue
		}
		// Move all four queried fields after the dense map. Field order among
		// them is repo, digest, media_type, artifact_type (logical 1,2,3,4).
		var head, tail []byte
		for d := f.Raw; len(d) > 0; {
			num, typ, n := protowire.ConsumeTag(d)
			k := protowire.ConsumeFieldValue(num, typ, d[n:])
			piece := d[:n+k]
			if num >= 8 && num <= 11 {
				tail = append(tail, piece...)
			} else {
				head = append(head, piece...)
			}
			d = d[n+k:]
		}
		raw := append(head, tail...)
		event := proto.Clone(f.Event).(*pdpb.WatchEvent)
		event.GetValue().Value = raw
		out = append(out, Fixture{"map256_prefix", event, raw})
	}
	return out
}

func CheckScenarios() error {
	for _, c := range ScanScenarios() {
		p, err := Compile(c.Query, false)
		if err != nil {
			return fmt.Errorf("%s compile: %w", c.Name, err)
		}
		native, err := Compile(c.Query, true)
		if err != nil {
			return err
		}
		for _, f := range ScanFixtures() {
			want, err := Eval(native, f.Event, nil)
			if err != nil {
				return err
			}
			for _, b := range Backends {
				newSession, err := b.Prepare(c.Fields)
				if err != nil {
					return err
				}
				for _, celMode := range []bool{false, true} {
					s, err := newSession(f.Raw)
					if err != nil {
						return err
					}
					var got bool
					if celMode {
						got, err = Eval(p, f.Event, s)
					} else {
						got, err = c.Direct(f.Event, s)
					}
					s.Close()
					if err != nil || got != want {
						return fmt.Errorf("%s/%s/%s cel%v: got%v want%v err%v", b.Name, f.Name, c.Name, celMode, got, want, err)
					}
				}
			}
		}
	}
	return nil
}
