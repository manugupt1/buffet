package buffet

import (
	"context"
	"strings"

	"github.com/gobuffalo/buffalo"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	olog "github.com/opentracing/opentracing-go/log"
)

var tracer opentracing.Tracer

// OpenTracing is a buffalo middleware that adds the necessary
// components to the request to make it traced through OpenTracing.
// Initialize it by passing in an opentracing.Tracer.
func OpenTracing(tr opentracing.Tracer) buffalo.MiddlewareFunc {
	tracer = tr
	return func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			opName := op(c)

			wireContext, err := tr.Extract(
				opentracing.HTTPHeaders,
				opentracing.HTTPHeadersCarrier(c.Request().Header))

			var sp opentracing.Span
			defer sp.Finish()
			// Create the span referring to the RPC client if available.
			// If wireContext == nil, a root span will be created.
			if err == nil {
				sp = tr.StartSpan(
					opName,
					ext.RPCServerOption(wireContext))
			} else {
				sp = tr.StartSpan(opName)
			}

			setTraceRequestOptions(&sp, c)
			// Setting span to the context is useful when SpanFromContext tries to create a new context
			ext.Component.Set(sp, "buffalo")
			c.Set("otspan", sp)

			err = next(c)
			if err != nil {
				ext.Error.Set(sp, true)
				sp.LogFields(olog.Error(err))
			}
			return err
		}
	}
}

// OpenTracingFromParentContext is a buffalo middleware that adds the necessary
// components to the request to make it traced through OpenTracing.
// Initialize it by passing in an opentracing.Tracer.
func OpenTracingFromParentContext(parent context.Context, values map[string]interface{}, tr opentracing.Tracer) buffalo.MiddlewareFunc {
	tracer = tr
	return func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			opName := op(c)

			wireSp, err := tr.Extract(
				opentracing.HTTPHeaders,
				opentracing.HTTPHeadersCarrier(c.Request().Header))

			var parentSp opentracing.Span
			var buffaloSp opentracing.Span
			// Create the span referring to the RPC client if available.
			// If wireSp == nil, a root span will be created.
			if err == nil {
				parentSp = tr.StartSpan(
					"parent",
					opentracing.ChildOf(wireSp))

			} else {
				parentSp = tr.StartSpan("root")
			}

			// Attach values to the parent tag
			for k, v := range values {
				parentSp.SetTag(k, v)
			}

			tr.StartSpan(opName, opentracing.ChildOf(parentSp.Context()))
			setTraceRequestOptions(&buffaloSp, c)
			// Setting span to the context is useful when SpanFromContext tries to create a new context
			ext.Component.Set(buffaloSp, "buffalo")
			c.Set("otspan", buffaloSp)

			err = next(c)
			if err != nil {
				ext.Error.Set(buffaloSp, true)
				buffaloSp.LogFields(olog.Error(err))
			}

			defer buffaloSp.Finish()
			defer parentSp.Finish()
			return err
		}
	}
}

// SpanFromContext attempts to retrieve a span from the Buffalo context,
// returning it if found.  If none is found a new one is created.
func SpanFromContext(c buffalo.Context) opentracing.Span {
	// fast path - find span in the buffalo context and return it
	sp := c.Value("otspan")
	if sp != nil {
		span, ok := sp.(opentracing.Span)
		if ok {
			c.LogField("span found", true)
			return span
		}
	}

	c.LogField("span found", false)
	// none exists, make a new one (sadface)
	opName := op(c)
	span := tracer.StartSpan(opName)
	setTraceRequestOptions(&span, c)
	return span

}

// ChildSpan returns a child span derived from the buffalo context "c"
func ChildSpan(opname string, c buffalo.Context) opentracing.Span {
	psp := SpanFromContext(c)
	sp := tracer.StartSpan(
		opname,
		opentracing.ChildOf(psp.Context()))
	return sp
}

func operation(s string) string {
	chunks := strings.Split(s, ".")
	return chunks[len(chunks)-1]
}

func op(c buffalo.Context) (opName string) {
	opName = "HTTP " + c.Request().Method + c.Request().URL.Path
	rt := c.Value("current_route")
	if rt != nil {
		route, ok := rt.(buffalo.RouteInfo)
		if ok {
			opName = operation(route.HandlerName)
		}
	}
	return
}

func setTraceRequestOptions(sp *opentracing.Span, c buffalo.Context) {
	ext.HTTPMethod.Set(*sp, c.Request().Method)
	ext.HTTPUrl.Set(*sp, c.Request().URL.String())
	ext.Component.Set(*sp, "buffalo")
}

func setTraceResponseOptions(sp *opentracing.Span, c buffalo.Context) {
	br, ok := c.Response().(*buffalo.Response)
	if ok {
		ext.HTTPStatusCode.Set(*sp, uint16(br.Status))
	}
}
