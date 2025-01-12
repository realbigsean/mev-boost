package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/eth/catalyst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHTTPServer struct {
	t               *testing.T
	statusCode      int
	expectedRequest string
	response        string
	reqCount        int
}

func (m *mockHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	require.Nil(m.t, err, "error reading body")

	assert.JSONEq(m.t, m.expectedRequest, string(body), "expected json body to be equal")

	w.WriteHeader(m.statusCode)
	w.Write([]byte(m.response))
	m.reqCount++
}

func newMockHTTPServer(t *testing.T, statusCode int, expectedRequest string, response string) (*mockHTTPServer, *httptest.Server) {
	server := &mockHTTPServer{
		t:               t,
		statusCode:      statusCode,
		expectedRequest: expectedRequest,
		response:        response,
	}

	return server, httptest.NewServer(server)
}

func TestNewRouter(t *testing.T) {
	_, mockHTTPServer := newMockHTTPServer(t, 200, "", "{}")

	type args struct {
		executionURL string
		relayURL     string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			"success",
			args{"http://foo", "http://bar"},
			false,
		},
		{
			"MockHTTPServer success",
			args{mockHTTPServer.URL, mockHTTPServer.URL},
			false,
		},
		{
			"fails with empty executionURL",
			args{"", "http://bar"},
			true,
		},
		{
			"fails with empty relayURL",
			args{"http://foo", ""},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRouter(tt.args.executionURL, tt.args.relayURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRouter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func formatRequestBody(method string, requestArray []interface{}) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"id":      "1",
		"jsonrpc": "2.0",
		"method":  method,
		"params":  requestArray,
	})
}

func formatResponse(responseResult interface{}) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"id":     "1",
		"error":  nil,
		"result": responseResult,
	})
}

type httpTest struct {
	name                        string
	requestArray                []interface{}
	expectedResponseResult      interface{}
	expectedStatusCode          int
	mockStatusCode              int
	expectedRequestsToExecution int
	expectedRequestsToRelay     int
}

func testHTTPMethod(t *testing.T, jsonRPCMethod string, tt *httpTest) {
	testHTTPMethodWithDifferentRPC(t, jsonRPCMethod, jsonRPCMethod, tt)
}

func testHTTPMethodWithDifferentRPC(t *testing.T, jsonRPCMethodCaller string, jsonRPCMethodProxy string, tt *httpTest) {
	t.Run(tt.name, func(t *testing.T) {
		// Format JSON-RPC body with the provided method and array of args
		body, err := formatRequestBody(jsonRPCMethodCaller, tt.requestArray)
		require.Nil(t, err, "error formatting json body")
		bodyProxy, err := formatRequestBody(jsonRPCMethodProxy, tt.requestArray)
		require.Nil(t, err, "error formatting json body")

		// Format JSON-RPC response
		resp, err := formatResponse(tt.expectedResponseResult)
		require.Nil(t, err, "error formatting json response")

		// Create mock http server that expects the above bodyProxy and returns the above response
		mockExecution, mockExecutionHTTP := newMockHTTPServer(t, tt.mockStatusCode, string(bodyProxy), string(resp))
		mockRelay, mockRelayHTTP := newMockHTTPServer(t, tt.mockStatusCode, string(bodyProxy), string(resp))

		// Create the router pointing at the mock server
		r, err := NewRouter(mockExecutionHTTP.URL, mockRelayHTTP.URL)
		require.Nil(t, err, "error creating router")

		// Craft a JSON-RPC request to the router
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Add("Content-Type", "application/json")
		w := httptest.NewRecorder()

		// Actually send the request, testing the router
		r.ServeHTTP(w, req)

		assert.JSONEq(t, string(resp), w.Body.String(), "expected response to be json equal")
		assert.Equal(t, tt.expectedStatusCode, w.Result().StatusCode, "expected status code to be equal")
		assert.Equal(t, tt.expectedRequestsToExecution, mockExecution.reqCount, "expected request count to execution to be equal")
		assert.Equal(t, tt.expectedRequestsToRelay, mockRelay.reqCount, "expected request count to relay to be equal")
	})
}

func strToBytes(s string) *hexutil.Bytes {
	ret := hexutil.Bytes(common.Hex2Bytes(s))
	return &ret
}
func TestMevService_ForckChoiceUpdated(t *testing.T) {
	b, e := json.Marshal([]interface{}{catalyst.PayloadAttributesV1{
		FeeRecipient: common.HexToAddress("0x0000000000000000000000000000000000000001"),
	}})
	fmt.Println(string(b), e)
	tests := []httpTest{
		{
			"basic success",
			[]interface{}{catalyst.ForkchoiceStateV1{}, catalyst.PayloadAttributesV1{
				FeeRecipient: common.HexToAddress("0x0000000000000000000000000000000000000001"),
			}},
			catalyst.ForkChoiceResponse{PayloadID: strToBytes("0x1")},
			200,
			200,
			1,
			1,
		},
	}
	for _, tt := range tests {
		testHTTPMethod(t, "engine_forkchoiceUpdatedV1", &tt)
	}
}

func TestMevService_ExecutePayload(t *testing.T) {
	tests := []httpTest{
		{
			"basic success",
			[]interface{}{ExecutionPayloadWithTxRootV1{
				BlockHash:     common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
				BaseFeePerGas: big.NewInt(4),
				Transactions:  [][]byte{},
			}},
			catalyst.ExecutePayloadResponse{
				Status: "VALID",
			},
			200,
			200,
			1,
			1,
		},
	}
	for _, tt := range tests {
		testHTTPMethod(t, "engine_executePayloadV1", &tt)
	}
}

func TestRelayService_ProposeBlindedBlockV1(t *testing.T) {
	tests := []httpTest{
		{
			"basic success",
			[]interface{}{SignedBlindedBeaconBlock{
				Message: &BlindedBeaconBlock{
					ParentRoot: "0x0000000000000000000000000000000000000000000000000000000000000001",
				},
				Signature: "0x0000000000000000000000000000000000000000000000000000000000000002",
			}},

			ExecutionPayloadWithTxRootV1{
				BlockHash:     common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
				BaseFeePerGas: big.NewInt(4),
				Transactions:  [][]byte{},
			},
			200,
			200,
			0,
			1,
		},
	}
	for _, tt := range tests {
		testHTTPMethod(t, "builder_proposeBlindedBlockV1", &tt)
	}
}

func TestRelayervice_GetPayloadHeaderV1(t *testing.T) {
	tests := []httpTest{
		{
			"basic success",
			[]interface{}{"0x1"},
			ExecutionPayloadWithTxRootV1{
				BlockHash:        common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
				BaseFeePerGas:    big.NewInt(4),
				TransactionsRoot: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002"),
			},
			200,
			200,
			1,
			1,
		},
	}
	for _, tt := range tests {
		testHTTPMethodWithDifferentRPC(t, "builder_getPayloadHeaderV1", "engine_getPayloadV1", &tt)
	}
}

func TestMevService_MethodFallback(t *testing.T) {
	tests := []httpTest{
		{
			"basic success",
			[]interface{}{"0x1"},
			[]string{"0x2"},
			200,
			200,
			1,
			0,
		},
	}
	for _, tt := range tests {
		testHTTPMethod(t, "engine_foo", &tt)
	}
}
