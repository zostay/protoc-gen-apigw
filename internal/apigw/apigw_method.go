package apigw

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	apigw_v1 "github.com/ductone/protoc-gen-apigw/apigw/v1"
	pgs "github.com/lyft/protoc-gen-star"
	pgsgo "github.com/lyft/protoc-gen-star/lang/go"
	"google.golang.org/protobuf/testing/protopack"
)

type methodTemplateContext struct {
	Name               string
	HTTPMethod         string
	Route              string
	FullMethodName     string
	MethodHandlerName  string
	DecoderHandlerName string
	HasBody            bool
	QueryParams        []*paramContext
	RouteParams        []*paramContext
	ServerName         string
	RequestType        string
	MethodName         string
}

type paramContext struct {
	ParamName           string
	Converter           string
	ConverterOutputName string
}

func jsonName(f pgs.Field) string {
	if f.Descriptor().JsonName != nil {
		return *f.Descriptor().JsonName
	}
	return f.Name().SnakeCase().String()
}

func (module *Module) path2fieldNumbers(path []string, msg pgs.Message) ([]protopack.Number, pgs.Field) {
	var lasField pgs.Field
	if len(path) == 0 {
		return nil, nil
	}
	rv := make([]protopack.Number, 0, len(path))
	next := path[0]
	deeper := path[1:]
	for _, f := range msg.Fields() {
		if next == jsonName(f) || next == f.Name().String() {
			lasField = f
			rv = append(rv, protopack.Number(f.Descriptor().GetNumber()))
			if len(deeper) >= 1 {
				nestedMsg := f.Type().Embed()
				if nestedMsg == nil {
					panic("apigw: getFieldNumbers: unexpected path: " + strings.Join(path, ".") + " on " + msg.FullyQualifiedName())
				}
				nums, edgeField := module.path2fieldNumbers(deeper, nestedMsg)
				lasField = edgeField
				rv = append(rv, nums...)
			}
			break
		}
	}
	if len(rv) == 0 {
		panic("apigw: getFieldNumbers: unexpected path: " + strings.Join(path, ".") + " on " + msg.FullyQualifiedName())
	}
	return rv, lasField
}

func isInt(pt pgs.ProtoType) bool {
	switch pt {
	case pgs.Int64T, pgs.SFixed64, pgs.SInt64, pgs.Int32T, pgs.SFixed32, pgs.SInt32, pgs.EnumT:
		return true
	default:
		return false
	}
}

func isUint(pt pgs.ProtoType) bool {
	switch pt {
	case pgs.UInt64T, pgs.Fixed64T, pgs.UInt32T, pgs.Fixed32T:
		return true
	default:
		return false
	}
}

func (module *Module) methodContext(ctx pgsgo.Context, w io.Writer, f pgs.File, service pgs.Service, method pgs.Method, ix *importTracker) (*methodTemplateContext, error) {
	vn := &varNamer{prefix: "vn", offset: 0}

	mext := &apigw_v1.MethodOptions{}
	_, err := method.Extension(apigw_v1.E_Method, mext)
	if err != nil {
		return nil, fmt.Errorf("apigw: methodContext: failed to extract Method extension from '%s': %w", method.FullyQualifiedName(), err)
	}
	if len(mext.Operations) == 0 {
		return nil, nil
	}

	// TODO(pquerna): support multiple routes to the same handler
	if len(mext.Operations) != 1 {
		return nil, fmt.Errorf("apigw: methodContext: only single operation bindings are currently supported: %v", mext.Operations)
	}
	operation := mext.Operations[0]
	if operation.Route == "" {
		return nil, fmt.Errorf("apigw: methodContext: operation.Route missing on '%s'", method.FullyQualifiedName())
	}

	ix.ProtobufProto = true
	ix.APIGWV1 = true
	ix.NetHTTP = true
	ix.GRPC = true

	// TODO(pquerna): this is like the Service raw name, but translate to Go-safe letters.
	serviceShortName := strings.TrimSuffix(ctx.Name(service).String(), "Server")

	parts, err := apigw_v1.ParseRoute(operation.Route)
	if err != nil {
		return nil, fmt.Errorf("apigw: methodContext: operation.Route invalid '%s': %w", method.FullyQualifiedName(), err)
	}

	rpc := make([]*paramContext, 0)
	for _, part := range parts {
		if !part.IsParam {
			continue
		}

		nesteFields := strings.Split(part.ParamName, ".")
		// TODO(pquerna): support nested fields
		if len(nesteFields) != 1 {
			module.Logf("apigw: methodContext: operation.Route invalid: target field is nested (unsupported right now) '%s' for route '%s'", method.FullyQualifiedName(), operation.Route)
			continue
		}

		nums, edgeField := module.path2fieldNumbers(nesteFields, method.Input())
		if len(nums) != 1 {
			return nil, fmt.Errorf("apigw: methodContext: operation.Route invalid: target numbers is nested (unsupported right now) '%s': %w", method.FullyQualifiedName(), err)
		}

		paramValueName := vn.String()
		vn = vn.Next()

		ix.ProtobufProtoPack = true
		routeGetter, err := templateExecToString("route_get_param.tmpl", &routeParseContext{
			ParamName:  part.ParamName,
			OutputName: paramValueName,
		})
		if err != nil {
			panic(err)
		}
		outName := vn.String()
		vn = vn.Next()

		fc, err := module.generateFieldConverter(method, nums[0], edgeField, ix, routeGetter, paramValueName, outName)
		if err != nil {
			panic(err)
		}
		fc.ParamName = part.ParamName
		rpc = append(rpc, fc)
	}

	qpc := make([]*paramContext, 0)
	for k, v := range operation.Query {
		// TODO: support nested fields
		nums, edgeField := module.path2fieldNumbers([]string{v}, method.Input())
		if len(nums) != 1 {
			return nil, fmt.Errorf("apigw: methodContext: operation.Query invalid: target is nested (unsupported right now) '%s': %w", method.FullyQualifiedName(), err)
		}
		paramValueName := vn.String()
		vn = vn.Next()

		ix.ProtobufProtoPack = true
		routeGetter, err := templateExecToString("query_get_param.tmpl", &routeParseContext{
			ParamName:  k,
			OutputName: paramValueName,
		})
		if err != nil {
			panic(err)
		}
		outName := vn.String()
		vn = vn.Next()

		fc, err := module.generateFieldConverter(method, nums[0], edgeField, ix, routeGetter, paramValueName, outName)
		if err != nil {
			panic(err)
		}
		fc.ParamName = k
		qpc = append(qpc, fc)
	}

	var httpMethod string
	switch operation.Method {
	case http.MethodGet:
		httpMethod = "http.MethodGet"
	case http.MethodHead:
		httpMethod = "http.MethodHead"
	case http.MethodPost:
		httpMethod = "http.MethodPost"
	case http.MethodPut:
		httpMethod = "http.MethodPut"
	case http.MethodPatch:
		httpMethod = "http.MethodPatch"
	case http.MethodDelete:
		httpMethod = "http.MethodDelete"
	default:
		return nil, fmt.Errorf("apigw: methodContext: operation.Method invalid '%s': %s", method.FullyQualifiedName(), operation.Method)
	}
	rv := &methodTemplateContext{
		Name:       method.FullyQualifiedName(),
		Route:      operation.Route,
		HTTPMethod: httpMethod,
		FullMethodName: fmt.Sprintf("%s_%s_FullMethodName",
			serviceShortName,
			ctx.Name(method).String(),
		),
		MethodHandlerName: fmt.Sprintf("_%s_%s_APIGW_Handler",
			serviceShortName,
			ctx.Name(method).String(),
		),
		DecoderHandlerName: fmt.Sprintf("_%s_%s_APIGW_Decoder",
			serviceShortName,
			ctx.Name(method).String(),
		),
		HasBody: operation.Method != "GET",

		ServerName:  ctx.ServerName(service).String(),
		RequestType: ctx.Name(method.Input()).String(),
		MethodName:  ctx.Name(method).String(),

		RouteParams: rpc,
		QueryParams: qpc,
	}
	if rv.HasBody {
		ix.Io = true
		ix.GRPCCodes = true
		ix.GRPCStatus = true
		ix.ProtobufEncodingJSON = true
	}
	return rv, nil
}

func (module *Module) generateFieldConverter(method pgs.Method, edgeNumber protopack.Number, edgeField pgs.Field,
	ix *importTracker,
	valueGetter string,
	inputName string,
	outputName string,
) (*paramContext, error) {
	switch {
	case edgeField.Type().IsRepeated():
		return nil, fmt.Errorf("apigw: methodContext: operation.Route invalid: target field is repeatd '%s'", method.FullyQualifiedName())
	case edgeField.Type().IsMap():
		return nil, fmt.Errorf("apigw: methodContext: operation.Route invalid: target field is map '%s'", method.FullyQualifiedName())
	case isInt(edgeField.Type().ProtoType()):
		ix.Strconv = true
		converter, err := templateExecToString("field_int.tmpl", &intFieldContext{
			FieldName:  jsonName(edgeField),
			Getter:     valueGetter,
			InputName:  inputName,
			OutputName: outputName,
			Tag:        edgeNumber,
		})
		if err != nil {
			panic(err)
		}
		return &paramContext{
			ConverterOutputName: outputName,
			Converter:           converter,
		}, nil
	case isUint(edgeField.Type().ProtoType()):
		ix.Strconv = true
		converter, err := templateExecToString("field_uint.tmpl", &uintFieldContext{
			FieldName:  jsonName(edgeField),
			Getter:     valueGetter,
			InputName:  inputName,
			OutputName: outputName,
			Tag:        edgeNumber,
		})
		if err != nil {
			panic(err)
		}
		return &paramContext{
			ConverterOutputName: outputName,
			Converter:           converter,
		}, nil
	case edgeField.Type().ProtoType() == pgs.StringT:
		converter, err := templateExecToString("field_string.tmpl", &stringFieldContext{
			FieldName:  jsonName(edgeField),
			Getter:     valueGetter,
			InputName:  inputName,
			OutputName: outputName,
			Tag:        edgeNumber,
		})
		if err != nil {
			panic(err)
		}
		return &paramContext{
			ConverterOutputName: outputName,
			Converter:           converter,
		}, nil
	case edgeField.Type().ProtoType() == pgs.BoolT:
		ix.Strconv = true
		converter, err := templateExecToString("field_bool.tmpl", &boolFieldContext{
			FieldName:  jsonName(edgeField),
			Getter:     valueGetter,
			InputName:  inputName,
			OutputName: outputName,
			Tag:        edgeNumber,
		})
		if err != nil {
			panic(err)
		}
		return &paramContext{
			ConverterOutputName: outputName,
			Converter:           converter,
		}, nil
	case edgeField.Type().ProtoType() == pgs.BytesT:
		return nil, fmt.Errorf("apigw: methodContext: operation.Route invalid: target field is bytes '%s'", method.FullyQualifiedName())
	default:
		return nil, fmt.Errorf("apigw: methodContext: operation.Route invalid: target field is unknown '%s'", method.FullyQualifiedName())
	}
}

type boolFieldContext struct {
	FieldName  string
	Getter     string
	OutputName string
	InputName  string
	Tag        protopack.Number
}

type stringFieldContext struct {
	FieldName  string
	Getter     string
	OutputName string
	InputName  string
	Tag        protopack.Number
}

type intFieldContext struct {
	FieldName  string
	Getter     string
	OutputName string
	InputName  string
	Tag        protopack.Number
}

type uintFieldContext struct {
	FieldName  string
	Getter     string
	OutputName string
	InputName  string
	Tag        protopack.Number
}

type routeParseContext struct {
	OutputName string
	ParamName  string
}
