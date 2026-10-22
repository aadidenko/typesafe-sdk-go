// Package typesafe evaluates named typed Questions against shared State using
// TypeSafe System One. Construct a reusable concurrent Client with NewClient,
// pass a context to each call, and inspect typed Answers and retained HTTP data.
//
// The SDK uses explicit configuration, does not load environment variables during
// construction, and never logs application content by default. Successful
// responses own their body bytes; callers need not close them.
package typesafe
