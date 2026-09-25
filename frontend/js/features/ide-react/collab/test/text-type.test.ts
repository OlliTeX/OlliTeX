import { describe, expect, it } from "vitest";

import { TEXT_TYPE } from "../text-type";

describe("collab/text-type (wire contract)", () => {
  it("pins the Y.Text type name the server seeds/versions", () => {
    // The Go collab server pins the same constant
    // (go/services/collab/roomdoc.go: TextType) and its unit tests pin the
    // server side; this test pins the client side. The two suites are the
    // two halves of one wire contract — rename only with both.
    expect(TEXT_TYPE).toBe("content");
  });
});
