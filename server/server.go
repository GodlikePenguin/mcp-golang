package server

import (
	"encoding/json"
	"fmt"
	"github.com/invopop/jsonschema"
	internaltools "github.com/metoro-io/mcp-golang/internal/tools"
	protocol2 "github.com/metoro-io/mcp-golang/protocol"
	transport2 "github.com/metoro-io/mcp-golang/transport"
	"reflect"
	"strings"
)

// Here we define the actual MCP server that users will create and run
// A server can be passed a number of handlers to handle requests from clients
// Additionally it can be parametrized by a transport. This transport will be used to actually send and receive messages.
// So for example if the stdio transport is used, the server will read from stdin and write to stdout
// If the SSE transport is used, the server will send messages over an SSE connection and receive messages from HTTP POST requests.

// The interface that we're looking to support is something like [gin](https://github.com/gin-gonic/gin)s interface

type toolResponseSent struct {
	Response *ToolResponse
	Error    error
}

// Custom JSON marshaling for ToolResponse
func (c toolResponseSent) MarshalJSON() ([]byte, error) {
	if c.Error != nil {
		errorText := c.Error.Error()
		c.Response = NewToolReponse(NewTextContent(errorText))
	}
	return json.Marshal(struct {
		Content []*Content `json:"content" yaml:"content" mapstructure:"content"`
		IsError bool       `json:"isError" yaml:"isError" mapstructure:"isError"`
	}{
		Content: c.Response.Content,
		IsError: c.Error != nil,
	})
}

// Custom JSON marshaling for ToolResponse
func (c resourceResponseSent) MarshalJSON() ([]byte, error) {
	if c.Error != nil {
		errorText := c.Error.Error()
		c.Response = NewResourceResponse(NewTextEmbeddedResource(c.Uri, errorText, "text/plain"))
	}
	return json.Marshal(c.Response)
}

type resourceResponseSent struct {
	Response *ResourceResponse
	Uri      string
	Error    error
}

func newResourceResponseSentError(err error) *resourceResponseSent {
	return &resourceResponseSent{
		Error: err,
	}
}

// newToolResponseSent creates a new toolResponseSent
func newResourceResponseSent(response *ResourceResponse) *resourceResponseSent {
	return &resourceResponseSent{
		Response: response,
	}
}

type promptResponseSent struct {
	Response *PromptResponse
	Error    error
}

func newPromptResponseSentError(err error) *promptResponseSent {
	return &promptResponseSent{
		Error: err,
	}
}

// newToolResponseSent creates a new toolResponseSent
func newPromptResponseSent(response *PromptResponse) *promptResponseSent {
	return &promptResponseSent{
		Response: response,
	}
}

// Custom JSON marshaling for PromptResponse
func (c promptResponseSent) MarshalJSON() ([]byte, error) {
	if c.Error != nil {
		errorText := c.Error.Error()
		c.Response = NewPromptResponse("error", NewPromptMessage(NewTextContent(errorText), RoleUser))
	}
	return json.Marshal(c)
}

type Server struct {
	transport          transport2.Transport
	tools              map[string]*tool
	prompts            map[string]*prompt
	resources          map[string]*resource
	serverInstructions *string
	serverName         string
	serverVersion      string
}

type prompt struct {
	Name              string
	Description       string
	Handler           func(BaseGetPromptRequestParamsArguments) *promptResponseSent
	PromptInputSchema *PromptSchema
}

type tool struct {
	Name            string
	Description     string
	Handler         func(BaseCallToolRequestParams) *toolResponseSent
	ToolInputSchema *jsonschema.Schema
}

type resource struct {
	Name        string
	Description string
	Uri         string
	mimeType    string
	Handler     func() *resourceResponseSent
}

func NewServer(transport transport2.Transport) *Server {
	return &Server{
		transport: transport,
		tools:     make(map[string]*tool),
		prompts:   make(map[string]*prompt),
		resources: make(map[string]*resource),
	}
}

// RegisterTool registers a new tool with the server
func (s *Server) RegisterTool(name string, description string, handler any) error {
	err := validateToolHandler(handler)
	if err != nil {
		return err
	}
	inputSchema := createJsonSchemaFromHandler(handler)

	s.tools[name] = &tool{
		Name:            name,
		Description:     description,
		Handler:         createWrappedToolHandler(handler),
		ToolInputSchema: inputSchema,
	}

	return nil
}

func (s *Server) RegisterResource(uri string, name string, description string, mimeType string, handler any) error {
	err := validateResourceHandler(handler)
	if err != nil {
		panic(err)
	}
	s.resources[uri] = &resource{
		Name:        name,
		Description: description,
		Uri:         uri,
		mimeType:    mimeType,
		Handler:     createWrappedResourceHandler(handler),
	}
	return nil
}

func createWrappedResourceHandler(userHandler any) func() *resourceResponseSent {
	handlerValue := reflect.ValueOf(userHandler)
	return func() *resourceResponseSent {
		// Call the handler with no arguments
		output := handlerValue.Call([]reflect.Value{})

		if len(output) != 2 {
			return newResourceResponseSentError(fmt.Errorf("handler must return exactly two values, got %d", len(output)))
		}

		if !output[0].CanInterface() {
			return newResourceResponseSentError(fmt.Errorf("handler must return a struct, got %s", output[0].Type().Name()))
		}
		promptR := output[0].Interface()
		if !output[1].CanInterface() {
			return newResourceResponseSentError(fmt.Errorf("handler must return an error, got %s", output[1].Type().Name()))
		}
		errorOut := output[1].Interface()
		if errorOut == nil {
			return newResourceResponseSent(promptR.(*ResourceResponse))
		}
		return newResourceResponseSentError(errorOut.(error))
	}
}

// We just want to check that handler takes no arguments and returns a ResourceResponse and an error
func validateResourceHandler(handler any) error {
	handlerValue := reflect.ValueOf(handler)
	handlerType := handlerValue.Type()
	if handlerType.NumIn() != 0 {
		return fmt.Errorf("handler must take no arguments, got %d", handlerType.NumIn())
	}
	if handlerType.NumOut() != 2 {
		return fmt.Errorf("handler must return exactly two values, got %d", handlerType.NumOut())
	}
	//if handlerType.Out(0) != reflect.TypeOf((*ResourceResponse)(nil)).Elem() {
	//	return fmt.Errorf("handler must return ResourceResponse, got %s", handlerType.Out(0).Name())
	//}
	//if handlerType.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
	//	return fmt.Errorf("handler must return error, got %s", handlerType.Out(1).Name())
	//}
	return nil
}

func (s *Server) RegisterPrompt(name string, description string, handler any) error {
	err := validatePromptHandler(handler)
	if err != nil {
		return err
	}
	promptSchema := createPromptSchemaFromHandler(handler)
	s.prompts[name] = &prompt{
		Name:              name,
		Description:       description,
		Handler:           createWrappedPromptHandler(handler),
		PromptInputSchema: promptSchema,
	}

	return nil
}

func createWrappedPromptHandler(userHandler any) func(BaseGetPromptRequestParamsArguments) *promptResponseSent {
	handlerValue := reflect.ValueOf(userHandler)
	handlerType := handlerValue.Type()
	argumentType := handlerType.In(0)
	return func(arguments BaseGetPromptRequestParamsArguments) *promptResponseSent {
		// Instantiate a struct of the type of the arguments
		if !reflect.New(argumentType).CanInterface() {
			return newPromptResponseSentError(fmt.Errorf("arguments must be a struct"))
		}
		unmarshaledArguments := reflect.New(argumentType).Interface()

		// Unmarshal the JSON into the correct type
		err := json.Unmarshal(arguments.Arguments, &unmarshaledArguments)
		if err != nil {
			return newPromptResponseSentError(fmt.Errorf("failed to unmarshal arguments: %w", err))
		}

		// Need to dereference the unmarshaled arguments
		of := reflect.ValueOf(unmarshaledArguments)
		if of.Kind() != reflect.Ptr || !of.Elem().CanInterface() {
			return newPromptResponseSentError(fmt.Errorf("arguments must be a struct"))
		}
		// Call the handler with the typed arguments
		output := handlerValue.Call([]reflect.Value{of.Elem()})

		if len(output) != 2 {
			return newPromptResponseSentError(fmt.Errorf("handler must return exactly two values, got %d", len(output)))
		}

		if !output[0].CanInterface() {
			return newPromptResponseSentError(fmt.Errorf("handler must return a struct, got %s", output[0].Type().Name()))
		}
		promptR := output[0].Interface()
		if !output[1].CanInterface() {
			return newPromptResponseSentError(fmt.Errorf("handler must return an error, got %s", output[1].Type().Name()))
		}
		errorOut := output[1].Interface()
		if errorOut == nil {
			return newPromptResponseSent(promptR.(*PromptResponse))
		}
		return newPromptResponseSentError(errorOut.(error))
	}
}

// Get the argument and iterate over the fields, we pull description from the jsonschema description tag
// We pull required from the jsonschema required tag
// Example:
// type Content struct {
// Title       string  `json:"title" jsonschema:"description=The title to submit,required"`
// Description *string `json:"description" jsonschema:"description=The description to submit"`
// }
// Then we get the jsonschema for the struct where Title is a required field and Description is an optional field
func createPromptSchemaFromHandler(handler any) *PromptSchema {
	handlerValue := reflect.ValueOf(handler)
	handlerType := handlerValue.Type()
	argumentType := handlerType.In(0)

	promptSchema := PromptSchema{
		Arguments: make([]PromptSchemaArgument, argumentType.NumField()),
	}

	for i := 0; i < argumentType.NumField(); i++ {
		field := argumentType.Field(i)
		fieldName := field.Name

		jsonSchemaTags := strings.Split(field.Tag.Get("jsonschema"), ",")
		var description *string
		var required = false
		for _, tag := range jsonSchemaTags {
			if strings.HasPrefix(tag, "description=") {
				s := strings.TrimPrefix(tag, "description=")
				description = &s
			}
			if tag == "required" {
				required = true
			}
		}

		promptSchema.Arguments[i] = PromptSchemaArgument{
			Name:        fieldName,
			Description: description,
			Required:    &required,
		}
	}
	return &promptSchema
}

// A prompt can only take a struct with fields of type string or *string as the argument
func validatePromptHandler(handler any) error {
	handlerValue := reflect.ValueOf(handler)
	handlerType := handlerValue.Type()
	argumentType := handlerType.In(0)

	if argumentType.Kind() != reflect.Struct {
		return fmt.Errorf("argument must be a struct")
	}

	for i := 0; i < argumentType.NumField(); i++ {
		field := argumentType.Field(i)
		isValid := false
		if field.Type.Kind() == reflect.String {
			isValid = true
		}
		if field.Type.Kind() == reflect.Ptr && field.Type.Elem().Kind() == reflect.String {
			isValid = true
		}
		if !isValid {
			return fmt.Errorf("all fields of the struct must be of type string or *string, found %s", field.Type.Kind())
		}
	}
	return nil
}

// Creates a full JSON schema from a user provided handler by introspecting the arguments
func createJsonSchemaFromHandler(handler any) *jsonschema.Schema {
	handlerValue := reflect.ValueOf(handler)
	handlerType := handlerValue.Type()
	argumentType := handlerType.In(0)
	inputSchema := jsonSchemaReflector.ReflectFromType(argumentType)
	return inputSchema
}

// This takes a user provided handler and returns a wrapped handler which can be used to actually answer requests
// Concretely, it will deserialize the arguments and call the user provided handler and then serialize the response
// If the handler returns an error, it will be serialized and sent back as a tool error rather than a protocol error
func createWrappedToolHandler(userHandler any) func(BaseCallToolRequestParams) *toolResponseSent {
	handlerValue := reflect.ValueOf(userHandler)
	handlerType := handlerValue.Type()
	argumentType := handlerType.In(0)
	return func(arguments BaseCallToolRequestParams) *toolResponseSent {
		// Instantiate a struct of the type of the arguments
		if !reflect.New(argumentType).CanInterface() {
			return newToolResponseSentError(fmt.Errorf("arguments must be a struct"))
		}
		unmarshaledArguments := reflect.New(argumentType).Interface()

		// Unmarshal the JSON into the correct type
		err := json.Unmarshal(arguments.Arguments, &unmarshaledArguments)
		if err != nil {
			return newToolResponseSentError(fmt.Errorf("failed to unmarshal arguments: %w", err))
		}

		// Need to dereference the unmarshaled arguments
		of := reflect.ValueOf(unmarshaledArguments)
		if of.Kind() != reflect.Ptr || !of.Elem().CanInterface() {
			return newToolResponseSentError(fmt.Errorf("arguments must be a struct"))
		}
		// Call the handler with the typed arguments
		output := handlerValue.Call([]reflect.Value{of.Elem()})

		if len(output) != 2 {
			return newToolResponseSentError(fmt.Errorf("handler must return exactly two values, got %d", len(output)))
		}

		if !output[0].CanInterface() {
			return newToolResponseSentError(fmt.Errorf("handler must return a struct, got %s", output[0].Type().Name()))
		}
		tool := output[0].Interface()
		if !output[1].CanInterface() {
			return newToolResponseSentError(fmt.Errorf("handler must return an error, got %s", output[1].Type().Name()))
		}
		errorOut := output[1].Interface()
		if errorOut == nil {
			return newToolResponseSent(tool.(*ToolResponse))
		}
		return newToolResponseSentError(errorOut.(error))
	}
}

func (s *Server) Serve() error {
	protocol := protocol2.NewProtocol(nil)
	protocol.SetRequestHandler("initialize", s.handleInitialize)
	protocol.SetRequestHandler("tools/list", s.handleListTools)
	protocol.SetRequestHandler("tools/call", s.handleToolCalls)
	protocol.SetRequestHandler("prompts/list", s.handleListPrompts)
	protocol.SetRequestHandler("prompts/get", s.handlePromptCalls)
	protocol.SetRequestHandler("resources/list", s.handleListResources)
	protocol.SetRequestHandler("resources/read", s.handleResourceCalls)
	return protocol.Connect(s.transport)
}

func (s *Server) handleInitialize(_ *transport2.BaseJSONRPCRequest, _ protocol2.RequestHandlerExtra) (interface{}, error) {
	return InitializeResult{
		Meta:            nil,
		Capabilities:    s.generateCapabilities(),
		Instructions:    s.serverInstructions,
		ProtocolVersion: "2024-11-05",
		ServerInfo: Implementation{
			Name:    s.serverName,
			Version: s.serverVersion,
		},
	}, nil
}

func (s *Server) handleListTools(_ *transport2.BaseJSONRPCRequest, _ protocol2.RequestHandlerExtra) (interface{}, error) {
	return internaltools.ToolsResponse{
		Tools: func() []internaltools.ToolRetType {
			var tools []internaltools.ToolRetType
			for _, tool := range s.tools {
				tools = append(tools, internaltools.ToolRetType{
					Name:        tool.Name,
					Description: &tool.Description,
					InputSchema: tool.ToolInputSchema,
				})
			}
			return tools
		}(),
	}, nil
}

func (s *Server) handleToolCalls(req *transport2.BaseJSONRPCRequest, _ protocol2.RequestHandlerExtra) (interface{}, error) {
	params := BaseCallToolRequestParams{}
	// Instantiate a struct of the type of the arguments
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
	}

	for name, tool := range s.tools {
		if name != params.Name {
			continue
		}
		return tool.Handler(params), nil
	}
	return nil, fmt.Errorf("unknown tool: %s", req.Method)
}

func (s *Server) generateCapabilities() ServerCapabilities {
	f := false
	return ServerCapabilities{
		Tools: func() *ServerCapabilitiesTools {
			if s.tools == nil {
				return nil
			}
			return &ServerCapabilitiesTools{
				ListChanged: &f,
			}
		}(),
	}
}

func (s *Server) handleListPrompts(request *transport2.BaseJSONRPCRequest, extra protocol2.RequestHandlerExtra) (interface{}, error) {
	return ListPromptsResult{
		Prompts: func() []*PromptSchema {
			var prompts []*PromptSchema
			for _, prompt := range s.prompts {
				prompts = append(prompts, prompt.PromptInputSchema)
			}
			return prompts
		}(),
	}, nil
}

func (s *Server) handleListResources(request *transport2.BaseJSONRPCRequest, extra protocol2.RequestHandlerExtra) (interface{}, error) {
	return ListResourcesResult{
		Resources: func() []*ResourceSchema {
			var resources []*ResourceSchema
			for _, resource := range s.resources {
				resources = append(resources, &ResourceSchema{
					Annotations: nil,
					Description: &resource.Description,
					MimeType:    &resource.mimeType,
					Name:        resource.Name,
					Uri:         resource.Uri,
				})
			}
			return resources
		}(),
	}, nil
}

func (s *Server) handlePromptCalls(req *transport2.BaseJSONRPCRequest, extra protocol2.RequestHandlerExtra) (interface{}, error) {
	params := BaseGetPromptRequestParamsArguments{}
	// Instantiate a struct of the type of the arguments
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
	}

	for name, prompter := range s.prompts {
		if name != params.Name {
			continue
		}
		return prompter.Handler(params), nil
	}
	return nil, fmt.Errorf("unknown prompt: %s", req.Method)
}

func (s *Server) handleResourceCalls(req *transport2.BaseJSONRPCRequest, extra protocol2.RequestHandlerExtra) (interface{}, error) {
	params := ReadResourceRequestParams{}
	// Instantiate a struct of the type of the arguments
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
	}

	for name, resource := range s.resources {
		if name != params.Uri {
			continue
		}
		return resource.Handler(), nil
	}
	return nil, fmt.Errorf("unknown prompt: %s", req.Method)
}

func validateToolHandler(handler any) error {
	handlerValue := reflect.ValueOf(handler)
	handlerType := handlerValue.Type()

	if handlerType.NumIn() != 1 {
		return fmt.Errorf("handler must take exactly one argument, got %d", handlerType.NumIn())
	}

	if handlerType.NumOut() != 2 {
		return fmt.Errorf("handler must return exactly two values, got %d", handlerType.NumOut())
	}

	// Check that the output type is *tools.ToolResponse
	if handlerType.Out(0) != reflect.PointerTo(reflect.TypeOf(ToolResponse{})) {
		return fmt.Errorf("handler must return *tools.ToolResponse, got %s", handlerType.Out(0).Name())
	}

	// Check that the output type is error
	if handlerType.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		return fmt.Errorf("handler must return error, got %s", handlerType.Out(1).Name())
	}

	return nil
}

var (
	jsonSchemaReflector = jsonschema.Reflector{
		BaseSchemaID:               "",
		Anonymous:                  true,
		AssignAnchor:               false,
		AllowAdditionalProperties:  true,
		RequiredFromJSONSchemaTags: true,
		DoNotReference:             true,
		ExpandedStruct:             true,
		FieldNameTag:               "",
		IgnoredTypes:               nil,
		Lookup:                     nil,
		Mapper:                     nil,
		Namer:                      nil,
		KeyNamer:                   nil,
		AdditionalFields:           nil,
		CommentMap:                 nil,
	}
)
