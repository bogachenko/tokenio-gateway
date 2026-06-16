package app

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

type migrationDatabaseFake struct {
	calls       []string
	applyErr    error
	validateErr error
}

func (f *migrationDatabaseFake) ApplyMigrations(context.Context) error {
	f.calls = append(f.calls, "apply")
	return f.applyErr
}

func (f *migrationDatabaseFake) ValidateSchema(context.Context) error {
	f.calls = append(f.calls, "validate")
	return f.validateErr
}

func (f *migrationDatabaseFake) Close() {
	f.calls = append(f.calls, "close")
}

func TestRunMigrationsAppliesValidatesAndCloses(t *testing.T) {
	database := &migrationDatabaseFake{}
	err := runMigrations(
		t.Context(),
		"postgres://tokenio",
		func(context.Context, string) (migrationDatabase, error) {
			database.calls = append(database.calls, "open")
			return database, nil
		},
	)
	if err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	want := []string{"open", "apply", "validate", "close"}
	if !reflect.DeepEqual(database.calls, want) {
		t.Fatalf("calls=%v want=%v", database.calls, want)
	}
}

func TestRunMigrationsStopsAfterApplyFailureAndCloses(t *testing.T) {
	applyErr := errors.New("apply failed")
	database := &migrationDatabaseFake{applyErr: applyErr}
	err := runMigrations(
		t.Context(),
		"postgres://tokenio",
		func(context.Context, string) (migrationDatabase, error) {
			return database, nil
		},
	)
	if !errors.Is(err, applyErr) {
		t.Fatalf("err=%v", err)
	}
	want := []string{"apply", "close"}
	if !reflect.DeepEqual(database.calls, want) {
		t.Fatalf("calls=%v want=%v", database.calls, want)
	}
}

func TestRunMigrationsClosesAfterValidationFailure(t *testing.T) {
	validateErr := errors.New("validate failed")
	database := &migrationDatabaseFake{validateErr: validateErr}
	err := runMigrations(
		t.Context(),
		"postgres://tokenio",
		func(context.Context, string) (migrationDatabase, error) {
			return database, nil
		},
	)
	if !errors.Is(err, validateErr) {
		t.Fatalf("err=%v", err)
	}
	want := []string{"apply", "validate", "close"}
	if !reflect.DeepEqual(database.calls, want) {
		t.Fatalf("calls=%v want=%v", database.calls, want)
	}
}

func TestRunMigrationsRejectsInvalidInputs(t *testing.T) {
	opener := func(context.Context, string) (migrationDatabase, error) {
		return &migrationDatabaseFake{}, nil
	}
	if err := runMigrations(nil, "postgres://tokenio", opener); err == nil {
		t.Fatal("expected nil-context error")
	}
	if err := runMigrations(t.Context(), " ", opener); err == nil {
		t.Fatal("expected blank-DSN error")
	}
	if err := runMigrations(t.Context(), "postgres://tokenio", nil); err == nil {
		t.Fatal("expected nil-opener error")
	}
}

func TestGatewayRuntimeDoesNotApplyMigrations(t *testing.T) {
	file, err := parser.ParseFile(
		token.NewFileSet(),
		"runtime.go",
		nil,
		0,
	)
	if err != nil {
		t.Fatalf("parse runtime.go: %v", err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "ApplyMigrations" {
			t.Errorf("gateway runtime must not apply migrations")
		}
		return true
	})
}
