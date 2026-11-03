package server

import (
 "bytes"
 "encoding/binary"
 "encoding/json"
 "path/filepath"
 "testing"

 "github.com/hm-gamesrv/cso-server-go/internal/config"
 "github.com/hm-gamesrv/cso-server-go/internal/protocol"
 "github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestReviewAutoCostumeWireExpiry(t *testing.T) {
 db, err := store.Open(filepath.Join(t.TempDir(), "wire.db3"))
 if err != nil { t.Fatal(err) }
 defer db.Close()
 if err = db.Register("wireuser", "secret1", "127.0.0.1", 0); err != nil { t.Fatal(err) }
 if err = db.CreateCharacter(1, "WireUser", config.DefaultUser{Level: 1}); err != nil { t.Fatal(err) }
 s := &Server{
  Store: db,
  Config: config.Config{InventorySlotMax: 100},
  items: map[int]itemDefinition{
   471: {
    Category: 12,
    ClassName: "HeadCostume",
   },
  },
 }
 c := &client{
  user: store.User{ID: 1},
  character: store.Character{
   ID: 1,
   Name: "WireUser",
  },
  queue: make(chan outbound, 64),
 }
 p := pendingReward{
  Label: itemReward{Title: "Review"},
  Grant: store.Grant{
   Items: []store.Reward{
    {
     ID: 471,
     Count: 1,
     Duration: 3,
     Dormant: true,
    },
   },
  },
 }
 payload, err := json.Marshal(p)
 if err != nil { t.Fatal(err) }
 progress, err := db.RecordQuestProgress(1, 1, 1, 1, 1, 1, payload)
 if err != nil { t.Fatal(err) }
 if err = s.finishRewardClaim(c, progress.Token, 0); err != nil { t.Fatal(err) }
 var expiry int
 if err = db.DB.QueryRow("SELECT expiryDate FROM UserInventory WHERE userID=1 AND itemID=471").Scan(&expiry); err != nil { t.Fatal(err) }
 marker := &protocol.Writer{}
 marker.U16(2)
 marker.U8(1)
 marker.U16(471)
 for len(c.queue) > 0 {
  msg := <-c.queue
  if msg.id != 154 { continue }
  at := bytes.Index(msg.data, marker.Data)
  if at < 0 { continue }
  wire := int(binary.LittleEndian.Uint32(msg.data[at+13:at+17]))
  if wire != expiry { t.Fatalf("inventory packet expiry=%d, database expiry=%d; auto-equip sent its stale snapshot", wire, expiry) }
  return
 }
 t.Fatal("costume inventory packet missing")
}
