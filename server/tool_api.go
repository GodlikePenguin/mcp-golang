package server

// This is a union type of all the different ToolResponse that can be sent back to the client.
// We allow creation through constructors only to make sure that the ToolResponse is valid.
type ToolResponse struct {
	Content []*Content
}

func NewToolReponse(content ...*Content) *ToolResponse {
	return &ToolResponse{
		Content: content,
	}
}
