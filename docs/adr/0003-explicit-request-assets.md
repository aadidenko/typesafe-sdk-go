# Keep media registration explicit and scoped to each request

State, instructions, and criteria accept ordinary JSON-compatible Go values, including structs, maps, and slices. Images are registered in a request's assets and referenced through strings in that content; a request-scoped helper can generate references. Automatically discovering image objects inside arbitrary Go values is not part of the public contract. Explicit registration preserves media placement and reuse without Python-style client-scoped attachment state or a second serialization system.

Image sources include bytes, files, base64, and `image.Image`, with multipart uploads. Images and Box questions require an explicit client or per-call experimental option. Without it, the SDK returns a local validation error; with it, the SDK supplies the required API header. The SDK does not select a different model automatically.
