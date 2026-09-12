package mongoh

import "testing"

func TestDBFromURI(t *testing.T) {
	cases := map[string]string{
		"mongodb://127.0.0.1/sharelatex": "sharelatex",
		"mongodb://host:27017/mydb":      "mydb",
		"mongodb://host:27017":           "sharelatex", // no path → default
		"mongodb+srv://a,b/others":       "others",
		"garbage":                        "sharelatex",
	}
	for uri, want := range cases {
		if got := DBFromURI(uri, "sharelatex"); got != want {
			t.Fatalf("DBFromURI(%q) = %q (want %q)", uri, got, want)
		}
	}
}

func TestWithDefaults_FillsURIAndDB(t *testing.T) {
	o := Options{URI: "mongodb://foo:27017/proddb", DB: ""}
	o.WithDefaults()
	if o.DB != "proddb" {
		t.Fatalf("DB = %q (want proddb)", o.DB)
	}
	if o.PingTimeout <= 0 {
		t.Fatal("PingTimeout should default to a positive value")
	}
}

func TestWithDefaults_DefaultURI(t *testing.T) {
	t.Setenv("MONGO_CONNECTION_STRING", "")
	t.Setenv("MONGO_HOST", "")
	o := Options{}
	o.WithDefaults()
	if o.DB != "sharelatex" {
		t.Fatalf("DB = %q (want sharelatex)", o.DB)
	}
	if o.URI != "mongodb://127.0.0.1/sharelatex" {
		t.Fatalf("URI = %q (want mongodb://127.0.0.1/sharelatex)", o.URI)
	}
}
