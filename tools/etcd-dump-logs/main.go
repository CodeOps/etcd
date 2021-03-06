package main

import (
	"flag"
	"fmt"
	"log"
	"path"
	"time"

	"github.com/coreos/etcd/etcdserver/etcdserverpb"
	"github.com/coreos/etcd/pkg/pbutil"
	"github.com/coreos/etcd/pkg/types"
	"github.com/coreos/etcd/raft/raftpb"
	"github.com/coreos/etcd/snap"
	"github.com/coreos/etcd/wal"
)

func main() {
	from := flag.String("data-dir", "", "")
	flag.Parse()
	if *from == "" {
		log.Fatal("Must provide -data-dir flag")
	}

	ss := snap.New(snapDir(*from))
	snapshot, err := ss.Load()
	var index uint64
	switch err {
	case nil:
		index = snapshot.Metadata.Index
		nodes := genIDSlice(snapshot.Metadata.ConfState.Nodes)
		fmt.Printf("Snapshot:\nterm=%d index=%d nodes=%s\n",
			snapshot.Metadata.Term, index, nodes)
	case snap.ErrNoSnapshot:
		fmt.Printf("Snapshot:\nempty\n")
	default:
		log.Fatalf("Failed loading snapshot: %v", err)
	}

	w, err := wal.Open(walDir(*from), index+1)
	if err != nil {
		log.Fatalf("Failed opening WAL: %v", err)
	}
	wmetadata, state, ents, err := w.ReadAll()
	w.Close()
	if err != nil {
		log.Fatalf("Failed reading WAL: %v", err)
	}
	id, cid := parseWALMetadata(wmetadata)
	vid := types.ID(state.Vote)
	fmt.Printf("WAL metadata:\nnodeID=%s clusterID=%s term=%d commitIndex=%d vote=%s\n",
		id, cid, state.Term, state.Commit, vid)

	fmt.Printf("WAL entries:\n")
	fmt.Printf("lastIndex=%d\n", ents[len(ents)-1].Index)
	fmt.Printf("%4s\t%10s\ttype\tdata\n", "term", "index")
	for _, e := range ents {
		msg := fmt.Sprintf("%4d\t%10d", e.Term, e.Index)
		switch e.Type {
		case raftpb.EntryNormal:
			msg = fmt.Sprintf("%s\tnorm", msg)
			var r etcdserverpb.Request
			if err := r.Unmarshal(e.Data); err != nil {
				msg = fmt.Sprintf("%s\t???", msg)
				break
			}
			switch r.Method {
			case "":
				msg = fmt.Sprintf("%s\tnoop", msg)
			case "SYNC":
				msg = fmt.Sprintf("%s\tmethod=SYNC time=%q", msg, time.Unix(0, r.Time))
			case "QGET", "DELETE":
				msg = fmt.Sprintf("%s\tmethod=%s path=%s", msg, r.Method, excerpt(r.Path, 64, 64))
			default:
				msg = fmt.Sprintf("%s\tmethod=%s path=%s val=%s", msg, r.Method, excerpt(r.Path, 64, 64), excerpt(r.Val, 128, 0))
			}
		case raftpb.EntryConfChange:
			msg = fmt.Sprintf("%s\tconf", msg)
			var r raftpb.ConfChange
			if err := r.Unmarshal(e.Data); err != nil {
				msg = fmt.Sprintf("%s\t???", msg)
			} else {
				msg = fmt.Sprintf("%s\tmethod=%s id=%s", msg, r.Type, types.ID(r.NodeID))
			}
		}
		fmt.Println(msg)
	}
}

func walDir(dataDir string) string { return path.Join(dataDir, "wal") }

func snapDir(dataDir string) string { return path.Join(dataDir, "snap") }

func parseWALMetadata(b []byte) (id, cid types.ID) {
	var metadata etcdserverpb.Metadata
	pbutil.MustUnmarshal(&metadata, b)
	id = types.ID(metadata.NodeID)
	cid = types.ID(metadata.ClusterID)
	return
}

func genIDSlice(a []uint64) []types.ID {
	ids := make([]types.ID, len(a))
	for i, id := range a {
		ids[i] = types.ID(id)
	}
	return ids
}

// excerpt replaces middle part with ellipsis and returns a double-quoted
// string safely escaped with Go syntax.
func excerpt(str string, pre, suf int) string {
	if pre+suf > len(str) {
		return fmt.Sprintf("%q", str)
	}
	return fmt.Sprintf("%q...%q", str[:pre], str[len(str)-suf:])
}
