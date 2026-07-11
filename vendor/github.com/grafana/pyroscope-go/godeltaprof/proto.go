package godeltaprof

type ProfileOptions struct {
	// if true - use runtime_FrameSymbolName - produces frames with generic types, for example [go.shape.int]
	// if false - use runtime.Frame->Function - produces frames with generic types omitted [...]
	GenericsFrames bool
	LazyMappings   bool
}
