package typesafe_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

// mustClient is only for the test goroutine; HTTP handlers use the nonfatal helpers below.
func mustClient(t *testing.T, config typesafe.Config) *typesafe.Client {
	t.Helper()

	client, err := typesafe.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	return client
}

func fixtureWrite(t *testing.T, writer io.Writer, body []byte) bool {
	t.Helper()

	if _, err := writer.Write(body); err != nil {
		t.Errorf("write fixture response: %v", err)

		return false
	}

	return true
}

func fixtureWriteString(t *testing.T, writer io.Writer, body string) bool {
	t.Helper()

	if _, err := io.WriteString(writer, body); err != nil {
		t.Errorf("write fixture response: %v", err)

		return false
	}

	return true
}

func fixtureDecode(t *testing.T, reader io.Reader, target any) bool {
	t.Helper()

	if err := json.NewDecoder(reader).Decode(target); err != nil {
		t.Errorf("decode fixture request: %v", err)

		return false
	}

	return true
}

func fixtureReadAll(t *testing.T, reader io.Reader) ([]byte, bool) {
	t.Helper()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Errorf("read fixture body: %v", err)

		return nil, false
	}

	return body, true
}

func fixtureDrain(t *testing.T, reader io.Reader) bool {
	t.Helper()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Errorf("drain fixture request: %v", err)

		return false
	}

	return true
}

func fixtureClose(t *testing.T, closer io.Closer) {
	t.Helper()

	if err := closer.Close(); err != nil {
		t.Errorf("close fixture resource: %v", err)
	}
}

func fixtureFlush(t *testing.T, writer http.ResponseWriter) bool {
	t.Helper()

	if err := http.NewResponseController(writer).Flush(); err != nil {
		t.Errorf("flush fixture response: %v", err)

		return false
	}

	return true
}

// awaitCallReady is called only by the test goroutine. Failure paths cancel and
// consume the client's result before terminating the test.
func awaitCallReady(t *testing.T, ready <-chan struct{}, result <-chan error, cancel context.CancelFunc) {
	t.Helper()

	select {
	case <-ready:
		return
	case err := <-result:
		cancel()
		t.Fatalf("call completed before fixture readiness: %v", err)
	case <-time.After(time.Second):
		cancel()

		select {
		case err := <-result:
			t.Fatalf("fixture readiness timed out; client joined after cancellation: %v", err)
		case <-time.After(time.Second):
			t.Fatal("fixture readiness timed out; client did not join after cancellation")
		}
	}
}
