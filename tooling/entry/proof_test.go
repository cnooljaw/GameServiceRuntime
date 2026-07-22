package entry

import (
	"errors"
	"testing"
)

func TestGatewayProofLineRoundTripAndFieldTampering(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")
	proof := SignGatewayProof(secret, GatewayProof{UID: "uid-1", SubID: "sub-1", Server: "asia", Generation: 7, Sequence: 9})
	line, err := FormatAuthLine(proof)
	if err != nil {
		t.Fatalf("FormatAuthLine() error = %v", err)
	}
	parsed, err := ParseAuthLine(line)
	if err != nil {
		t.Fatalf("ParseAuthLine() error = %v", err)
	}
	if !VerifyGatewayProof(secret, parsed) {
		t.Fatal("VerifyGatewayProof() = false, want true")
	}
	parsed.Server = "other"
	if VerifyGatewayProof(secret, parsed) {
		t.Fatal("VerifyGatewayProof(tampered) = true, want false")
	}
}

func TestParseAuthLineRejectsAmbiguousOrOversizedInputs(t *testing.T) {
	for _, line := range []string{
		"AUTH a b c 01 1 d\n",
		"AUTH a b c 1 0 d\n",
		"AUTH a b c 1 1 d extra\n",
		"AUTH a b c 1 1 d\r\n",
	} {
		if _, err := ParseAuthLine(line); !errors.Is(err, ErrInvalidProof) {
			t.Fatalf("ParseAuthLine(%q) error = %v, want ErrInvalidProof", line, err)
		}
	}
}
