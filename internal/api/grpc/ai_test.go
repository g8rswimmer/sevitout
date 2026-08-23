package grpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/g8rswimmer/sevitout/internal/ai"
	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// fakeRunner is a grpchandler.AIRunner that returns canned results instead
// of running internal/ai.Dispatcher's real provider-calling logic.
type fakeRunner struct {
	output    *store.AIOutput
	err       error
	chunks    []ai.Chunk
	streamErr error
}

func (f *fakeRunner) Run(_ context.Context, sevID string, action ai.Action, _ int64) (*store.AIOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := *f.output
	out.SEVID = sevID
	out.Action = string(action)
	return &out, nil
}

func (f *fakeRunner) StreamOne(_ context.Context, _ string, _ ai.Action, _ int64) (<-chan ai.Chunk, error) {
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	ch := make(chan ai.Chunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func newTestAIServer(runner grpchandler.AIRunner) (*grpchandler.AIServer, *memory.AIOutputStore, *memory.AIPluginStore) {
	outputs := memory.NewAIOutputStore()
	plugins := memory.NewAIPluginStore()
	return grpchandler.NewAIServer(runner, outputs, plugins), outputs, plugins
}

func TestTriggerAction_Success(t *testing.T) {
	runner := &fakeRunner{output: &store.AIOutput{ID: 1, TriggerEvent: "manual", Content: "a summary", CreatedAt: time.Now()}}
	server, _, _ := newTestAIServer(runner)

	resp, err := server.TriggerAction(context.Background(), &pb.TriggerActionRequest{
		SevId: "SEV-2026-0001", Action: pb.AIAction_AI_ACTION_SUMMARIZE,
	})
	if err != nil {
		t.Fatalf("TriggerAction: %v", err)
	}
	if resp.GetContent() != "a summary" || resp.GetAction() != pb.AIAction_AI_ACTION_SUMMARIZE {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestTriggerAction_MissingSEVID(t *testing.T) {
	server, _, _ := newTestAIServer(&fakeRunner{})
	_, err := server.TriggerAction(context.Background(), &pb.TriggerActionRequest{Action: pb.AIAction_AI_ACTION_SUMMARIZE})
	if grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", grpcCode(err))
	}
}

func TestTriggerAction_UnspecifiedAction(t *testing.T) {
	server, _, _ := newTestAIServer(&fakeRunner{})
	_, err := server.TriggerAction(context.Background(), &pb.TriggerActionRequest{SevId: "SEV-2026-0001"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", grpcCode(err))
	}
}

func TestTriggerAction_NoRunnerConfigured(t *testing.T) {
	server, _, _ := newTestAIServer(nil)
	_, err := server.TriggerAction(context.Background(), &pb.TriggerActionRequest{
		SevId: "SEV-2026-0001", Action: pb.AIAction_AI_ACTION_SUMMARIZE,
	})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition", grpcCode(err))
	}
}

func TestTriggerAction_MapsSentinelErrors(t *testing.T) {
	cases := []struct {
		err  error
		want codes.Code
	}{
		{ai.ErrAIDisabledForSEV, codes.FailedPrecondition},
		{ai.ErrPluginDisabled, codes.FailedPrecondition},
		{ai.ErrNoEnabledPlugin, codes.FailedPrecondition},
		{ai.ErrRateLimited, codes.ResourceExhausted},
		{store.ErrNotFound, codes.NotFound},
		{errors.New("boom"), codes.Internal},
	}
	for _, c := range cases {
		runner := &fakeRunner{err: c.err}
		server, _, _ := newTestAIServer(runner)
		_, err := server.TriggerAction(context.Background(), &pb.TriggerActionRequest{
			SevId: "SEV-2026-0001", Action: pb.AIAction_AI_ACTION_SUMMARIZE,
		})
		if grpcCode(err) != c.want {
			t.Errorf("err=%v: code = %v, want %v", c.err, grpcCode(err), c.want)
		}
	}
}

// fakeStreamActionServer stands in for pb.AIService_StreamActionServer,
// recording every chunk sent. Embedding a nil grpc.ServerStream is safe:
// AIServer.StreamAction only ever calls Send and Context, both overridden.
type fakeStreamActionServer struct {
	grpc.ServerStream
	sent []*pb.AIActionChunk
}

func (f *fakeStreamActionServer) Send(c *pb.AIActionChunk) error {
	f.sent = append(f.sent, c)
	return nil
}

func (f *fakeStreamActionServer) Context() context.Context { return context.Background() }

func TestStreamAction_ForwardsChunks(t *testing.T) {
	runner := &fakeRunner{chunks: []ai.Chunk{{Content: "part one "}, {Content: "part one part two", Done: true}}}
	server, _, _ := newTestAIServer(runner)
	stream := &fakeStreamActionServer{}

	err := server.StreamAction(&pb.TriggerActionRequest{SevId: "SEV-2026-0001", Action: pb.AIAction_AI_ACTION_SUMMARIZE}, stream)
	if err != nil {
		t.Fatalf("StreamAction: %v", err)
	}
	if len(stream.sent) != 2 {
		t.Fatalf("got %d chunks, want 2", len(stream.sent))
	}
	if !stream.sent[1].GetDone() || stream.sent[1].GetContent() != "part one part two" {
		t.Fatalf("unexpected final chunk: %+v", stream.sent[1])
	}
}

func TestStreamAction_NoRunnerConfigured(t *testing.T) {
	server, _, _ := newTestAIServer(nil)
	err := server.StreamAction(&pb.TriggerActionRequest{SevId: "SEV-2026-0001", Action: pb.AIAction_AI_ACTION_SUMMARIZE}, &fakeStreamActionServer{})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition", grpcCode(err))
	}
}

func TestListOutputs_ReturnsStoredOutputs(t *testing.T) {
	server, outputs, _ := newTestAIServer(&fakeRunner{})
	if err := outputs.Create(context.Background(), &store.AIOutput{SEVID: "SEV-2026-0001", Action: "summarize", Content: "x"}); err != nil {
		t.Fatalf("seed output: %v", err)
	}

	resp, err := server.ListOutputs(context.Background(), &pb.ListAIOutputsRequest{SevId: "SEV-2026-0001"})
	if err != nil {
		t.Fatalf("ListOutputs: %v", err)
	}
	if len(resp.GetOutputs()) != 1 {
		t.Fatalf("got %d outputs, want 1", len(resp.GetOutputs()))
	}
}

func TestListOutputs_MissingSEVID(t *testing.T) {
	server, _, _ := newTestAIServer(&fakeRunner{})
	_, err := server.ListOutputs(context.Background(), &pb.ListAIOutputsRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", grpcCode(err))
	}
}

func TestListPlugins_OnlyEnabled(t *testing.T) {
	server, _, plugins := newTestAIServer(&fakeRunner{})
	if err := plugins.Create(context.Background(), &store.AIPlugin{Name: "on", Enabled: true, HandlerType: store.AIHandlerBuiltin}); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}
	if err := plugins.Create(context.Background(), &store.AIPlugin{Name: "off", Enabled: false, HandlerType: store.AIHandlerBuiltin}); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}

	resp, err := server.ListPlugins(context.Background(), &pb.ListPluginsRequest{})
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(resp.GetPlugins()) != 1 || resp.GetPlugins()[0].GetName() != "on" {
		t.Fatalf("got %+v, want only the enabled plugin", resp.GetPlugins())
	}
}
