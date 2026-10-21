package safety

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewStorePrivacyCapacityAndCancellation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reviews")
	s := &ReviewStore{Dir: dir, Limit: 1}
	summary, target, digest, err := Summarize(json.RawMessage(`{"id":9223372036854775807,"password":"private-secret","xml":"private-xml"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(summary, "private-secret") || strings.Contains(summary, "private-xml") || !strings.Contains(target, "9223372036854775807") {
		t.Fatal("bad redaction or numeric precision")
	}
	r := Review{Tool: "fixture", Area: "notes", Level: Medium, Target: target, ArgumentsSHA256: digest, Arguments: summary, ExpectedImpact: "Change fixture", Checks: []string{"validated"}}
	path, err := s.Save(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dir, path} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatal("nonprivate review")
		}
	}
	s.Complete(path, "completed")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "completed") {
		t.Fatal("missing outcome")
	}
	if _, err := s.Save(context.Background(), r); err == nil {
		t.Fatal("capacity exceeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Save(ctx, r); err == nil {
		t.Fatal("cancelled review saved")
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(context.Background(), r); err == nil {
		t.Fatal("public directory accepted")
	}
	if _, err := (&ReviewStore{Dir: "relative"}).Save(context.Background(), r); err == nil {
		t.Fatal("relative directory accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (&ReviewStore{Dir: link}).Save(context.Background(), r); err == nil {
		t.Fatal("symlink accepted")
	}
}
