package hookdedup

import (
	"testing"
)

func TestClaimRejectsConcurrentEvent(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	claimed, release, err := Claim("codex", "session-1")
	if err != nil || !claimed {
		t.Fatalf("首次 Claim = claimed:%v err:%v", claimed, err)
	}
	defer release()

	claimed, _, err = Claim("codex", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Error("并发的同一事件获得了第二个占位")
	}
}

func TestClaimAllowsNextEventAfterRelease(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	claimed, release, err := Claim("codex", "session-2")
	if err != nil || !claimed {
		t.Fatalf("首次 Claim = claimed:%v err:%v", claimed, err)
	}
	release()

	claimed, release, err = Claim("codex", "session-2")
	if err != nil || !claimed {
		t.Fatalf("释放后的 Claim = claimed:%v err:%v", claimed, err)
	}
	release()
}
