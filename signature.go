// httpsignatures is a golang implementation of the http-signatures spec
// found at https://tools.ietf.org/html/draft-cavage-http-signatures
package httpsignatures

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var (
	ErrorNoSignatureHeader = errors.New("No Signature header found in request")

	signatureRegex = regexp.MustCompile(`(\w+)="([^"]*)"`)
)

type SignatureParameters struct {
	KeyID     string
	Algorithm *Algorithm
	Headers   HeaderList
	Signature string
}

// FromRequest takes the signature string from the HTTP-Request
// both Signature and Authorization http headers are supported.
func (s *SignatureParameters) fromRequest(r *http.Request) error {
	var httpSignatureString string
	if sig, ok := r.Header[HeaderSignature]; ok {
		httpSignatureString = sig[0]
	} else {
		if h, ok := r.Header[HeaderAuthorization]; ok {
			httpSignatureString = strings.TrimPrefix(h[0], authScheme)
		} else {
			return ErrorNoSignatureHeader
		}
	}
	err := s.fromString(httpSignatureString)
	if err != nil {
		return err
	}
	s.loadHeaders(r)
	return nil
}

func (s *SignatureParameters) loadHeaders(r *http.Request) error {
	for header := range s.Headers {
		if header == RequestTarget {
			if tl, err := requestTargetLine(r); err == nil {
				s.Headers[header] = tl
			} else {
				return fmt.Errorf("Missing required target line '%s'", header)
			}
		} else {
			if value := r.Header.Get(header); value != "" {
				s.Headers[header] = value
			} else {
				return fmt.Errorf("Missing required header '%s'", header)
			}
		}
	}
	return nil
}

// FromConfig takes the string configuration and fills the
// SignatureParameters struct
func (s *SignatureParameters) fromConfig(keyId string, algorithm string, headers []string) error {
	if len(keyId) == 0 {
		return errors.New("Missing keyId")
	}
	if len(algorithm) == 0 {
		return errors.New("Missing algorithm")
	}
	s.KeyID = keyId

	alg, err := algorithmFromString(algorithm)
	if err != nil {
		return err
	}
	s.Algorithm = alg

	if len(headers) == 0 {
		s.Headers = HeaderList{"date": ""}
	} else {
		s.Headers = HeaderList{}
		for _, header := range headers {
			s.Headers[header] = ""
		}
	}

	return nil
}

// FromString creates a new Signature from its encoded form,
// eg `keyId="a",algorithm="b",headers="c",signature="d"`
func (s *SignatureParameters) fromString(in string) error {
	var key string
	var value string
	*s = SignatureParameters{}

	for _, m := range signatureRegex.FindAllStringSubmatch(in, -1) {
		key = m[1]
		value = m[2]

		if key == "keyId" {
			s.KeyID = value
		} else if key == "algorithm" {
			alg, err := algorithmFromString(value)
			if err != nil {
				return err
			}
			s.Algorithm = alg
		} else if key == "headers" {
			s.Headers.fromString(value)
		} else if key == "signature" {
			s.Signature = value
		}
		// ignore unknown parameters
	}

	if len(s.Headers) == 0 {
		s.Headers = HeaderList{"date": ""}
	}

	if len(s.Signature) == 0 {
		return errors.New("Missing signature")
	}

	if len(s.KeyID) == 0 {
		return errors.New("Missing keyId")
	}

	if s.Algorithm == nil {
		return errors.New("Missing algorithm")
	}

	return nil
}

// String returns the encoded form of the Signature
func (s SignatureParameters) hTTPSignatureString(signature string) string {
	str := fmt.Sprintf(
		`keyId="%s",algorithm="%s"`,
		s.KeyID,
		s.Algorithm.Name,
	)

	if len(s.Headers) > 0 {
		str += fmt.Sprintf(`,headers="%s"`, s.Headers.toString())
	}

	str += fmt.Sprintf(`,signature="%s"`, signature)

	return str
}

func (s SignatureParameters) calculateSignature(keyB64 string, signingString string) (string, error) {
	byteKey, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return "", err
	}

	hash, err := s.Algorithm.Sign(&byteKey, []byte(signingString))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(*hash), err
}

// Verify verifies this signature for the given base64 encodedkey
func (s SignatureParameters) Verify(keyBase64 string, signingString string) (bool, error) {
	byteKey, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return false, err
	}

	byteSignature, err := base64.StdEncoding.DecodeString(s.Signature)
	if err != nil {
		return false, err
	}
	result, err := s.Algorithm.Verify(&byteKey, []byte(signingString), &byteSignature)
	if err != nil {
		return false, err
	}

	return result, nil
}

// HeaderList contains headers
type HeaderList map[string]string

// FromString constructs a headerlist from the 'headers' string
func (h *HeaderList) fromString(list string) {
	*h = HeaderList{}
	list = strings.TrimSpace(list)
	headers := strings.Split(strings.ToLower(string(list)), " ")
	for _, header := range headers {
		// init header map with empty string
		(*h)[header] = ""
	}
}

func (h HeaderList) toString() string {
	list := ""
	for header := range h {
		list += " " + strings.ToLower(header)
	}
	return list
}

func (h HeaderList) signingString(req *http.Request) (string, error) {
	lines := []string{}

	for header := range h {
		if header == RequestTarget {
			reqTarget, err := requestTargetLine(req)
			if err != nil {
				return "", err
			}
			lines = append(lines, reqTarget)
		} else {
			line, err := headerLine(req, header)
			if err != nil {
				return "", err
			}
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n"), nil
}

func requestTargetLine(req *http.Request) (string, error) {
	var url, method string
	if req.URL == nil {
		return "", fmt.Errorf("URL not in Request")
	}
	if len(req.Method) == 0 {
		return "", fmt.Errorf("Method not in Request")
	}

	url = req.URL.RequestURI()
	method = strings.ToLower(req.Method)
	return fmt.Sprintf("%s: %s %s", RequestTarget, method, url), nil
}

func headerLine(req *http.Request, header string) (string, error) {
	if value := req.Header.Get(header); value != "" {
		return fmt.Sprintf("%s: %s", header, value), nil
	}
	return "", fmt.Errorf("Missing required header '%s'", header)
}
