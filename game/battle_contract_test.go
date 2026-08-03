package game

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

func TestBattlePublicContractHasNoGenericEpochOrRevision(t *testing.T) {
	for _, value := range []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "BattleConfig", typeOf: reflect.TypeOf(BattleConfig{})},
		{name: "BattleSnapshot", typeOf: reflect.TypeOf(BattleSnapshot{})},
	} {
		for _, field := range []string{"Epoch", "Revision"} {
			if _, exists := value.typeOf.FieldByName(field); exists {
				t.Fatalf("%s still exposes generic %s", value.name, field)
			}
		}
	}

	contextType := reflect.TypeOf((*BattleContext)(nil)).Elem()
	for _, method := range []string{"Epoch", "Revision"} {
		if _, exists := contextType.MethodByName(method); exists {
			t.Fatalf("BattleContext still exposes generic %s", method)
		}
	}

	file, err := parser.ParseFile(token.NewFileSet(), "types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec := specification.(*ast.TypeSpec)
			if typeSpec.Name.Name == "BattleEpoch" || typeSpec.Name.Name == "BattleRevision" {
				t.Fatalf("game still exports generic %s", typeSpec.Name.Name)
			}
		}
	}
}
