package icmp

import "testing"

func TestMakePayloadSize(t *testing.T) {
	// Below minimum is padded up.
	if got := len(makePayload(0)); got != 8 {
		t.Errorf("makePayload(0) len = %d, want 8", got)
	}
	if got := len(makePayload(56)); got != 56 {
		t.Errorf("makePayload(56) len = %d, want 56", got)
	}
}

func TestReplyKindString(t *testing.T) {
	cases := map[ReplyKind]string{
		KindTimeout:         "timeout",
		KindEchoReply:       "echo-reply",
		KindTimeExceeded:    "time-exceeded",
		KindDestUnreachable: "dest-unreachable",
		KindError:           "error",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", k, got, want)
		}
	}
}
