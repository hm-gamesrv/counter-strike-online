package server
import("time";"path/filepath";"io";"log/slog";"testing";"github.com/hm-gamesrv/cso-server-go/internal/config";"github.com/hm-gamesrv/cso-server-go/internal/store")
func reviewMatch(t *testing.T)(*Server,*room,*matchStat){t.Helper();db,e:=store.Open(filepath.Join(t.TempDir(),"review.db3"));if e!=nil{t.Fatal(e)};t.Cleanup(func(){db.Close()});db.Register("review","secret1","127.0.0.1",0);db.CreateCharacter(1,"Review",config.DefaultUser{Level:1});c:=pipeClient(t,1,"Review");c.playing=true;c.team=2;c.room=1;stat:=&matchStat{user:c,class:10,kills:200,items:[]store.Item{{ID:10,InUse:1,Status:1},{ID:73,InUse:1,Status:1}}};r:=&room{id:1,host:c,members:[]*client{c},match:&gameMatch{id:"review",mode:4,mapID:1,stats:map[int]*matchStat{1:stat}}};s:=&Server{Store:db,Log:slog.New(slog.NewTextHandler(io.Discard,nil)),rooms:map[int]*room{1:r}};s.Config.GameMatch.CalcResults.GameModeCoefficient=map[string]struct{Exp int;Points int}{"4":{Exp:100,Points:100}};s.Config.GameMatch.CalcResults.BonusPercentage.Classes=map[string]struct{Exp int;Points int}{"10":{Exp:50,Points:50}};return s,r,stat}
func TestReviewBonusBeforeCap(t *testing.T){s,r,_:=reviewMatch(t);if e:=s.endGame(r,true);e!=nil{t.Fatal(e)};ch,e:=s.Store.Character(1);if e!=nil{t.Fatal(e)};if ch.Exp!=19999{t.Fatalf("earned=%d; baseline min(20000,9999)+20000*50/100=19999",ch.Exp)}}
func TestReviewBonusRetryPercent(t *testing.T){s,r,stat:=reviewMatch(t);s.Config.GameMatch.CalcResults.BonusPercentage.Items=map[string]struct{Exp int;Points int}{"73":{Exp:50,Points:50}};_,e:=s.Store.DB.Exec("CREATE TRIGGER failReward BEFORE UPDATE ON UserCharacter BEGIN SELECT RAISE(ABORT,'injected'); END");if e!=nil{t.Fatal(e)};if e=s.endGame(r,true);e==nil{t.Fatal("expected injected failure")};s.Store.DB.Exec("DROP TRIGGER failReward");if e=s.endGame(r,true);e!=nil{t.Fatal(e)};if stat.itemExp!=50{t.Fatalf("item percentage after one failed settlement and retry=%d, want 50",stat.itemExp)}}

func TestReviewReconnectOfflineMinutes(t *testing.T){
 s,r,_:=reviewMatch(t)
 host:=pipeClient(t,2,"Host");host.room=1;host.playing=true;r.host=host;r.members=[]*client{host};delete(r.match.stats,1);r.settings.setNumber(8,8,1)
 s.users=map[int]*client{2:host}
 now:=time.Now()
 s.restore=map[int]reconnectSlot{1:{roomID:1,matchID:"review",playing:true,expires:now.Add(time.Minute),hasStat:true,stat:reconnectStat{started:now.Add(-4*time.Minute),questMinutes:1}}}
 s.quests=[]questTask{{QuestID:1,TaskID:1,Goal:100,Conditions:[]questCondition{{EventType:0,GoalPoints:1}}}}
 c:=pipeClient(t,1,"Review");c.inLobby=true;s.restoreSession(c);s.tickQuestTime(now)
 progress,e:=s.Store.QuestProgress(1,1,1);if e!=nil{t.Fatal(e)}
 if progress.Units!=0{t.Fatalf("offline time awarded quest minutes=%d; want 0 after reconnect",progress.Units)}
}
