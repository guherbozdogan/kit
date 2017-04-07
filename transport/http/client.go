package http

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io/ioutil"
	"net/http"
	"net/url"

	"golang.org/x/net/context/ctxhttp"

	"github.com/go-kit/kit/endpoint"
	frame "github.com/guherbozdogan/mesos-go-http-client/client/frame"
)

// Client wraps a URL and provides a method that implements endpoint.Endpoint.
type Client struct {
	client         *http.Client
	method         string
	tgt            *url.URL
	enc            EncodeRequestFunc
	dec            DecodeResponseFunc
	decFrame  frame.FrameReadFunc //add/guherbozdogan/04/07/17 : for streaming responses
	before         []RequestFunc
	after          []ClientResponseFunc
	afterFrame []ClientResponseFunc  //add/guherbozdogan/04/07/17 : for streaming responses
	bufferedStream bool
	frameIO  frame.FrameIO  //add/guherbozdogan/04/07/17 : for streaming responses
	frameIOErrFunc frame.ErrorFunc //add/guherbozdogan/04/08/17 : for streaming responses
}


// NewClient constructs a usable Client for a single remote method.
func NewClient(
	method string,
	tgt *url.URL,
	enc EncodeRequestFunc,
	dec DecodeResponseFunc,
	decFrame frame.FrameReadFunc,
	frameIOType frame.FrameIOType ,  //add/guherbozdogan/04/07/17 : for streaming responses
	frameIOErrFunc frame.ErrorFunc, //add/guherbozdogan/04/08/17 : for streaming responses
	options ...ClientOption,
) *Client {
	c := &Client{
		client:         http.DefaultClient,
		method:         method,
		tgt:            tgt,
		enc:            enc,
		dec:            dec,
		before:         []RequestFunc{},
		after:          []ClientResponseFunc{},
		afterFrame:          []ClientResponseFunc{},
		decFrame:            decFrame,
		bufferedStream: false,
		frameIO: nil,
		frameIOErrFunc : frameIOErrFunc	}
	if c.bufferedStream {
		c.frameIO=frame.NewFrameIO(frameIOType)
	}
	for _, option := range options {
		option(c)
	}
	return c
}

// ClientOption sets an optional parameter for clients.
type ClientOption func(*Client)

// SetClient sets the underlying HTTP client used for requests.
// By default, http.DefaultClient is used.
func SetClient(client *http.Client) ClientOption {
	return func(c *Client) { c.client = client }
}

// ClientBefore sets the RequestFuncs that are applied to the outgoing HTTP
// request before it's invoked.
func ClientBefore(before ...RequestFunc) ClientOption {
	return func(c *Client) { c.before = append(c.before, before...) }
}

// ClientAfter sets the ClientResponseFuncs applied to the incoming HTTP
// request prior to it being decoded. This is useful for obtaining anything off
// of the response and adding onto the context prior to decoding.
func ClientAfter(after ...ClientResponseFunc) ClientOption {
	return func(c *Client) { c.after = append(c.after, after...) }
}

// ClientAfterFrame  sets the ClientResponseFuncs applied to the incoming HTTP
// response frame prior to it being decoded. This is useful for obtaining anything off
// of the response's frame and adding onto the context prior to decoding.
func ClientAfterFrame(after ...ClientResponseFunc) ClientOption {
	return func(c *Client) { c.after = append(c.afterFrame, after...) }
}

// BufferedStream sets whether the Response.Body is left open, allowing it
// to be read from later. Useful for transporting a file as a buffered stream.
func BufferedStream(buffered bool) ClientOption {
	return func(c *Client) { c.bufferedStream = buffered }
}

func (c Client) BufferedStreamHandler(ctx context.Context,r  *http.Response){
	
	ctx, cancel := context.WithCancel(ctx)
	if !c.bufferedStream {
		defer cancel()
	}
	
	
	c.frameIO.Read(ctx, r.Body,c.decFrame, c.frameIOErrFunc)
		

	
	
	//some error handling for err.
}


// Endpoint returns a usable endpoint that invokes the remote endpoint.
func (c Client) Endpoint() endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		ctx, cancel := context.WithCancel(ctx)
		if !c.bufferedStream {
		defer cancel()
		}

		req, err := http.NewRequest(c.method, c.tgt.String(), nil)
		if err != nil {
			return nil, err
		}

		if err = c.enc(ctx, req, request); err != nil {
			return nil, err
		}

		for _, f := range c.before {
			ctx = f(ctx, req)
		}

		resp, err := ctxhttp.Do(ctx, c.client, req)
		if err != nil {
			return nil, err
		}
		if !c.bufferedStream {
			defer resp.Body.Close()
		}
		

		for _, f := range c.after {
			ctx = f(ctx, resp)
		}
		

		

		response, err := c.dec(ctx, resp)
		if err != nil {
			return nil, err
		}
		if c.bufferedStream {
			ctxTmp := context.WithValue(context.Background(), "response", resp)
			go c.BufferedStreamHandler(ctxTmp, resp)
		}

		return response, nil
	}
}

// EncodeJSONRequest is an EncodeRequestFunc that serializes the request as a
// JSON object to the Request body. Many JSON-over-HTTP services can use it as
// a sensible default. If the request implements Headerer, the provided headers
// will be applied to the request.
func EncodeJSONRequest(c context.Context, r *http.Request, request interface{}) error {
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	if headerer, ok := request.(Headerer); ok {
		for k := range headerer.Headers() {
			r.Header.Set(k, headerer.Headers().Get(k))
		}
	}
	var b bytes.Buffer
	r.Body = ioutil.NopCloser(&b)
	return json.NewEncoder(&b).Encode(request)
}

// EncodeXMLRequest is an EncodeRequestFunc that serializes the request as a
// XML object to the Request body. If the request implements Headerer,
// the provided headers will be applied to the request.
func EncodeXMLRequest(c context.Context, r *http.Request, request interface{}) error {
	r.Header.Set("Content-Type", "text/xml; charset=utf-8")
	if headerer, ok := request.(Headerer); ok {
		for k := range headerer.Headers() {
			r.Header.Set(k, headerer.Headers().Get(k))
		}
	}
	var b bytes.Buffer
	r.Body = ioutil.NopCloser(&b)
	return xml.NewEncoder(&b).Encode(request)
}
