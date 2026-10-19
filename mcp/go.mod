module example.com/icloud-notes-mcp

go 1.26

replace github.com/torkve/icloud-notes => ./third_party/icloud-notes

require github.com/torkve/icloud-notes v1.0.0

require (
	github.com/google/uuid v1.6.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
