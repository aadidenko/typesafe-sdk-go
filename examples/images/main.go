// Images and Box are experimental; this example runs only when called explicitly.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: images path/to/image.png")
	}

	photo, err := typesafe.ImageFromFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	var assets typesafe.Assets

	reference := assets.Add(photo)

	client, err := typesafe.NewClient(typesafe.Config{APIKey: os.Getenv("TYPESAFE_API_KEY"), Model: "jev-latest", ExperimentalImages: true})
	if err != nil {
		log.Fatal(err)
	}

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State: map[string]any{"photo": reference}, Assets: assets,
		Questions: typesafe.Questions{"location": typesafe.BoxQuestion{Instructions: "Locate the largest object in " + reference}},
	})
	if err != nil {
		log.Fatal(err)
	}

	box, err := response.Box("location")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("top=%g bottom=%g left=%g right=%g confidence=%v\n", box.Box.Top, box.Box.Bottom, box.Box.Left, box.Box.Right, box.Confidence)
}
