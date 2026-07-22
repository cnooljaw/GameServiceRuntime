package entry

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

const (
	proofPrefix = "GSR-Gateway-Proof-v1\n"
	maxAuthLine = 4096
)

// SignGatewayProof computes the RFC-0290 HMAC for valid proof fields.
func SignGatewayProof(secret []byte, proof GatewayProof) GatewayProof {
	if len(secret) < sha256.Size || !validProofFields(proof) {
		proof.MAC = nil
		return proof
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(gatewayProofInput(proof))
	proof.MAC = mac.Sum(nil)
	return proof
}

// VerifyGatewayProof reports whether proof is valid for secret using constant-time HMAC comparison.
func VerifyGatewayProof(secret []byte, proof GatewayProof) bool {
	if len(secret) < sha256.Size || !validProofFields(proof) || len(proof.MAC) != sha256.Size {
		return false
	}
	expected := SignGatewayProof(secret, GatewayProof{UID: proof.UID, SubID: proof.SubID, Server: proof.Server, Generation: proof.Generation, Sequence: proof.Sequence})
	return hmac.Equal(expected.MAC, proof.MAC)
}

// FormatAuthLine returns the exact RFC-0290 Gateway AUTH line for proof.
func FormatAuthLine(proof GatewayProof) (string, error) {
	if !validProofFields(proof) || len(proof.MAC) != sha256.Size {
		return "", ErrInvalidProof
	}
	line := "AUTH " + encodedText(proof.UID) + " " + encodedText(proof.SubID) + " " + encodedText(proof.Server) + " " + strconv.FormatUint(proof.Generation, 10) + " " + strconv.FormatUint(proof.Sequence, 10) + " " + base64.RawURLEncoding.EncodeToString(proof.MAC) + "\n"
	if len(line) > maxAuthLine {
		return "", ErrInvalidProof
	}
	return line, nil
}

// ParseAuthLine parses exactly one RFC-0290 Gateway AUTH line.
func ParseAuthLine(line string) (GatewayProof, error) {
	if len(line) == 0 || len(line) > maxAuthLine || !strings.HasSuffix(line, "\n") || strings.ContainsRune(line, '\r') {
		return GatewayProof{}, ErrInvalidProof
	}
	fields := strings.Split(strings.TrimSuffix(line, "\n"), " ")
	if len(fields) != 7 || fields[0] != "AUTH" {
		return GatewayProof{}, ErrInvalidProof
	}
	uid, ok := decodedText(fields[1])
	if !ok {
		return GatewayProof{}, ErrInvalidProof
	}
	subID, ok := decodedText(fields[2])
	if !ok {
		return GatewayProof{}, ErrInvalidProof
	}
	server, ok := decodedText(fields[3])
	if !ok {
		return GatewayProof{}, ErrInvalidProof
	}
	generation, ok := canonicalUint(fields[4])
	if !ok {
		return GatewayProof{}, ErrInvalidProof
	}
	sequence, ok := canonicalUint(fields[5])
	if !ok {
		return GatewayProof{}, ErrInvalidProof
	}
	mac, err := base64.RawURLEncoding.DecodeString(fields[6])
	if err != nil || len(mac) != sha256.Size {
		return GatewayProof{}, ErrInvalidProof
	}
	proof := GatewayProof{UID: uid, SubID: subID, Server: server, Generation: generation, Sequence: sequence, MAC: mac}
	if !validProofFields(proof) {
		return GatewayProof{}, ErrInvalidProof
	}
	return proof, nil
}

func gatewayProofInput(proof GatewayProof) []byte {
	return []byte(proofPrefix + "uid=" + encodedText(proof.UID) + "\nsubid=" + encodedText(proof.SubID) + "\nserver=" + encodedText(proof.Server) + "\ngeneration=" + strconv.FormatUint(proof.Generation, 10) + "\nsequence=" + strconv.FormatUint(proof.Sequence, 10) + "\n")
}

func encodedText(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }

func decodedText(value string) (string, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", false
	}
	text := string(decoded)
	return text, validText(text)
}

func canonicalUint(value string) (uint64, bool) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return 0, false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	return parsed, err == nil && parsed != 0
}
