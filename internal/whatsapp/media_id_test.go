package whatsapp

import "testing"

// Run-2 needs-validation closure: an inbound media id is
// interpolated into the Graph URL path under the process
// token, so only the genuine id shape — decimal digits —
// may ever reach the request builder. A crafted id is
// refused before any request exists.
func TestDownloadMediaBytesRefusesCraftedIDs(t *testing.T) {
	t.Setenv("WHATSAPP_TOKEN", "test-token")
	for _, id := range []string{
		"", "abc", "123/456", "../123", "123?x=1", "123#frag",
		"12 34", "123%2f456", "１２３", "-1", "1.5",
	} {
		if _, _, ok := DownloadMediaBytes(id); ok {
			t.Fatalf("crafted media id %q must be refused", id)
		}
	}
}
