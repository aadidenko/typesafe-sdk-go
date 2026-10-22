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
	// Omit Compression to use zstd for complete bodies of at least 1 KiB.
	// Gzip can be convenient for gateways supporting standard gzip tooling.
	client, err := typesafe.NewClient(typesafe.Config{APIKey: os.Getenv("TYPESAFE_API_KEY"), Compression: typesafe.CompressionGzip})
	if err != nil {
		log.Fatal(err)
	}
	request := typesafe.SystemOneRequest{State: strings.Repeat("This is an example passage. ", 100), Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "Is this text in English?"}}}
	// Select none for this call without changing the client's gzip preference.
	response, err := client.SystemOne(context.Background(), request, typesafe.WithCompression(typesafe.CompressionNone))
	if err != nil {
		log.Fatal(err)
	}
	answer, err := response.Noul("q")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(answer.Probability)
}
