package interactive

import (
	"testing"
)

func TestListFilesRegex(t *testing.T) {

	cmdCombinedOutput = func(name string, arg ...string) ([]byte, error) {
		return []byte(`LICENSE
Readme.md
background.js
content.js
icon_128.png
icon_16.png
icon_48.png
manifest.json
popup.html
popup.js
screenshots/wikipedia.webp
settings.html
settings.js
`), nil

	}

	a := ListFilesArgs{
		Pattern: ".*\\.(js|html|css)$",
	}

	s, err := a.Run()
	if err != nil {
		t.Fatal(err)
	}

	expect := `background.js
content.js
popup.html
popup.js
settings.html
settings.js
`
	if s != expect {
		t.Fatalf("got %q expected %q", s, expect)
	}

}
