package main

import (
	"fmt"
	"github.com/metoro-io/mcp-golang/server"
	"github.com/metoro-io/mcp-golang/transport/stdio"
)

type Content struct {
	Title       string  `json:"title" jsonschema:"required,description=The title to submit"`
	Description *string `json:"description" jsonschema:"description=The description to submit"`
}
type MyFunctionsArguments struct {
	Submitter string  `json:"submitter" jsonschema:"required,description=The name of the thing calling this tool (openai, google, claude, etc)"`
	Content   Content `json:"content" jsonschema:"required,description=The content of the message"`
}

func main() {
	done := make(chan struct{})

	s := server.NewServer(stdio.NewStdioServerTransport())
	err := s.RegisterTool("hello", "Say hello to a person", func(arguments MyFunctionsArguments) (*server.ToolResponse, error) {
		return server.NewToolReponse(server.NewTextContent(fmt.Sprintf("Hello, %s!", arguments.Submitter))), nil
	})
	if err != nil {
		panic(err)
	}

	err = s.RegisterPrompt("promt_test", "This is a test prompt", func(arguments Content) (*server.PromptResponse, error) {
		return server.NewPromptResponse("description", server.NewPromptMessage(server.NewTextContent(fmt.Sprintf("Hello, %s!", arguments.Title)), server.RoleUser)), nil
	})
	if err != nil {
		panic(err)
	}

	err = s.RegisterResource("test://resource", "resource_test", "This is a test resource", "application/json", func() (*server.ResourceResponse, error) {
		return server.NewResourceResponse(server.NewTextEmbeddedResource("test://resource", "This is a test resource", "application/json")), nil
	})

	err = s.RegisterResource("file://app_logs", "app_logs", "The app logs", "text/plain", func() (*server.ResourceResponse, error) {
		return server.NewResourceResponse(server.NewTextEmbeddedResource("file://app_logs", "This is a test resource", "text/plain")), nil
	})

	err = s.Serve()
	if err != nil {
		panic(err)
	}

	<-done
}
