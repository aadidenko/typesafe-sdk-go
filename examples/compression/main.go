// Compression configuration is compiled by normal tests. Running this example
// makes an explicitly requested live evaluation and needs an external API key.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Omit Compression to use zstd for complete bodies of at least 1 KiB.
	// Gzip can be convenient for gateways supporting standard gzip tooling.
	client, err := typesafe.NewClient(typesafe.Config{APIKey: os.Getenv("TYPESAFE_API_KEY"), Compression: typesafe.CompressionGzip})
	if err != nil {
		return err
	}

	request := typesafe.SystemOneRequest{State: strings.Repeat("This is an example passage. ", 100), Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "Is this text in English?"}}}
	// Select none for this call without changing the client's gzip preference.
	response, err := client.SystemOne(context.Background(), request, typesafe.WithCompression(typesafe.CompressionNone))
	if err != nil {
		return err
	}

	answer, err := response.Noul("q")
	if err != nil {
		return err
	}

	if _, err := fmt.Println(answer.Probability); err != nil {
		return err
	}

	return nil
}
