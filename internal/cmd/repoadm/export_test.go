package repoadm

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestStore(t *testing.T) {

	s := store{
		path: "/tmp/test.db",
	}

	if err := s.connect(); err != nil {
		t.Errorf("%s\n", err)
	}
	defer s.disconnect()

	if err := s.init(); err != nil {
		t.Errorf("%s\n", err)
	}

	for i := 0; i <= 3; i++ {
		um := Umap{
			UIDRepo:  fmt.Sprintf("u000000%d@ru.nl", i),
			UIDLocal: fmt.Sprintf("user%d", i),
			Email:    fmt.Sprintf("user%d@dccn.nl", i),
		}
		if err := s.set("umap", um.UIDRepo, &um); err != nil {
			t.Errorf("%s\n", err)
		}
	}

	for i := 0; i <= 3; i++ {
		um := Umap{}
		if err := s.get("umap", fmt.Sprintf("u000000%d@ru.nl", i), &um); err != nil {
			t.Errorf("%s\n", err)
		}
		t.Logf("%+v\n", um)
	}

	ums := make(map[string]interface{})
	if err := s.getAll("umap", ums); err != nil {
		t.Errorf("%s\n", err)
	}
	for k, v := range ums {
		t.Logf("key:%s, value: %+v\n", k, v)
	}

	for i := 0; i <= 3; i++ {
		col := CollExport{
			Path: fmt.Sprintf("/.repo/dccn/ds_%d", i),
			OU:   "dccn",
			ViewersRepo: []string{
				"u0000001@ru.nl",
				"u0000003@ru.nl",
			},
		}
		if err := s.set("cmap", filepath.Base(col.Path), &col); err != nil {
			t.Errorf("%s\n", err)
		}
	}
	cms := make(map[string]interface{})
	if err := s.getAll("cmap", cms); err != nil {
		t.Errorf("%s\n", err)
	}
	for k, v := range cms {
		t.Logf("key:%s, value: %+v\n", k, v)
	}

}
