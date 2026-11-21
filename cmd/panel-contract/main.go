// panel-contract generates the frontend boundary types from the Go authority.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"github.com/sck03/sing-box-vpn/internal/manage"
	"github.com/sck03/sing-box-vpn/internal/runtimeconfig"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func main() {
	check := flag.Bool("check", false, "fail when generated contracts are stale")
	output := flag.String("output", "internal/manage/panel/contracts.d.ts", "generated file")
	flag.Parse()
	types := map[string]string{}
	var describe func(reflect.Type) string
	name := func(t reflect.Type) string {
		n := t.Name()
		if strings.HasSuffix(t.PkgPath(), "/subscription") {
			n = "Subscription" + n
		}
		if strings.HasSuffix(t.PkgPath(), "/serverconfig") {
			n = "Server" + n
		}
		return n
	}
	describe = func(t reflect.Type) string {
		if t == reflect.TypeFor[json.RawMessage]() {
			return "unknown"
		}
		switch t.Kind() {
		case reflect.Pointer:
			return describe(t.Elem()) + " | null"
		case reflect.Bool:
			return "boolean"
		case reflect.String:
			return "string"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
			return "number"
		case reflect.Interface:
			return "unknown"
		case reflect.Map:
			return "Record<string, " + describe(t.Elem()) + "> | null"
		case reflect.Slice, reflect.Array:
			if t.Elem().Kind() == reflect.Uint8 {
				return "string"
			}
			return "Array<" + describe(t.Elem()) + "> | null"
		case reflect.Struct:
			if t.PkgPath() == "time" && t.Name() == "Time" {
				return "string"
			}
			n := name(t)
			if _, exists := types[n]; exists {
				return n
			}
			types[n] = ""
			if t == reflect.TypeFor[application.TaskCommand]() {
				operations := make([]string, 0, len(application.CommandFields))
				for operation := range application.CommandFields {
					operations = append(operations, operation)
				}
				sort.Strings(operations)
				var variants []string
				for _, operation := range operations {
					fields := []string{fmt.Sprintf("operation: %q", operation)}
					for i := 1; i < t.NumField(); i++ {
						f := t.Field(i)
						key := strings.Split(f.Tag.Get("json"), ",")[0]
						allowed := slices.Contains(application.CommandFields[operation], key)
						fieldType := "never"
						if allowed {
							fieldType = describe(f.Type)
						}
						if key == "config" && operation == "gateway" {
							fieldType = `{ op: "start" | "stop" | "swap" | "refresh" }`
						}
						if key == "value" && operation == "service" {
							fieldType = `"start" | "stop" | "restart" | "enable" | "disable"`
						}
						if key == "value" && operation == "bbr" {
							fieldType = `"enable" | "restore"`
						}
						if key == "account" && operation == "account" {
							fieldType += ` & { operation: "bind" | "rotate" | "unbind"; client?: never; tuic_port?: never }`
						}
						if key == "subscription" && operation == "subscription" {
							fieldType += ` & { operation: "refresh" | "check" | "deliver" }`
						}
						optional := "?"
						if allowed && key != "value" {
							optional = ""
						}
						if (operation == "service" || operation == "bbr") && key == "value" {
							optional = ""
						}
						fields = append(fields, key+optional+": "+fieldType)
					}
					variants = append(variants, "{ "+strings.Join(fields, "; ")+" }")
				}
				types[n] = "export type TaskCommand =\n  | " + strings.Join(variants, "\n  | ") + ";\n"
				return n
			}
			var body strings.Builder
			parents := []string{}
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if f.Anonymous && f.Type.Kind() == reflect.Struct {
					parents = append(parents, describe(f.Type))
				}
			}
			extends := ""
			if len(parents) > 0 {
				extends = " extends " + strings.Join(parents, ", ")
			}
			fmt.Fprintf(&body, "export interface %s%s {\n", n, extends)
			if t == reflect.TypeFor[manage.App]() {
				fmt.Fprintf(&body, "  schema: %d;\n", manage.AppSchema)
			}
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if !f.IsExported() || f.Anonymous {
					continue
				}
				tag := strings.Split(f.Tag.Get("json"), ",")
				if tag[0] == "-" {
					continue
				}
				key := tag[0]
				if key == "" {
					key = f.Name
				}
				optional := ""
				if len(tag) > 1 {
					optional = "?"
				}
				valueType := describe(f.Type)
				if t == reflect.TypeFor[manage.PlatformConfig]() && f.Name == "Schema" {
					valueType = fmt.Sprint(manage.PlatformSchema)
				}
				fmt.Fprintf(&body, "  %s%s: %s;\n", key, optional, valueType)
			}
			body.WriteString("}\n")
			types[n] = body.String()
			return n
		default:
			panic("unsupported contract type: " + t.String())
		}
	}
	for _, t := range []reflect.Type{
		reflect.TypeFor[manage.AccountSummary](),
		reflect.TypeFor[application.RefreshResult](),
		reflect.TypeFor[application.DeliveryResult](),
		reflect.TypeFor[manage.SubscriptionStatusResponse](),
		reflect.TypeFor[sub.TemplateImport](), reflect.TypeFor[sub.TemplateImportResult](), reflect.TypeFor[manage.OverviewSnapshot](), reflect.TypeFor[manage.ProgramStatus](), reflect.TypeFor[manage.CertificatePreparation](), reflect.TypeFor[manage.CertificateImport](),
		reflect.TypeFor[manage.ManagedNode](), reflect.TypeFor[manage.App](), reflect.TypeFor[manage.APIError](), reflect.TypeFor[sub.AccountOutputOption](),
		reflect.TypeFor[manage.PlatformConfig](), reflect.TypeFor[manage.ConfigurationSave](), reflect.TypeFor[manage.ConfigurationState](), reflect.TypeFor[manage.QueuedTask](), reflect.TypeFor[application.TaskCommand](),
		reflect.TypeFor[domain.Account](), reflect.TypeFor[domain.Credential](), reflect.TypeFor[accounting.Usage](), reflect.TypeFor[accounting.Grant](),
		reflect.TypeFor[application.AccountCommand](), reflect.TypeFor[application.AccountExportRequest](), reflect.TypeFor[manage.AccountExportResponse](), reflect.TypeFor[application.SubscriptionCommand](), reflect.TypeFor[controlstore.Task](), reflect.TypeFor[adminauth.Session](), reflect.TypeFor[runtimeconfig.Capabilities](),
		reflect.TypeFor[manage.CredentialSummary](), reflect.TypeFor[accounting.CredentialPublication](), reflect.TypeFor[controlstore.TaskPage](), reflect.TypeFor[controlstore.TaskCounts](),
	} {
		describe(t)
	}
	var data bytes.Buffer
	data.WriteString("// Generated by go run ./cmd/panel-contract; do not edit.\n\n")
	keys := make([]string, 0, len(types))
	for key := range types {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		if i > 0 {
			data.WriteByte('\n')
		}
		data.WriteString(types[key])
	}
	if *check {
		actual, err := os.ReadFile(*output)
		if err != nil || !bytes.Equal(actual, data.Bytes()) {
			fmt.Fprintln(os.Stderr, "panel contracts are stale; run go run ./cmd/panel-contract")
			os.Exit(1)
		}
	} else if err := os.WriteFile(*output, data.Bytes(), 0644); err != nil {
		panic(err)
	}
}
