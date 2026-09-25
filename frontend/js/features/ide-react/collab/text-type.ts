// The Y.Text type name holding the room's document content.
//
// THIS CONSTANT IS PART OF THE WIRE CONTRACT: it must equal the Go collab
// server's `collab.TextType` (go/services/collab/roomdoc.go):
//
//	const TextType = "content"
//
// The server seeds, reads, versions and restores `doc.getText(TEXT_TYPE)`,
// and the roomdoc unit tests pin the server side of the constant. If this
// value changes, both sides change in the same commit — do not rename one.
export const TEXT_TYPE = "content";
