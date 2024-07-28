# Raft

> 本项目是基于 MIT6.824 课程实验lab2 实现 Raft 分布式一致性协议。
>
> 这里罗列了一些重要的参考资料，包括课程讲义中列出的、课程助教的博客和网上一些同学的分享记录等等。
>
> 1. 作者讲解：https://www.youtube.com/watch?v=YbZ3zDzDnrw
> 2. 论文原文：[In Search of an Understandable Consensus Algorithm (Extended Version)](https://raft.github.io/raft.pdf)
> 3. 可视化 1：http://thesecretlivesofdata.com/raft/
> 4. 可视化 2：https://observablehq.com/@stwind/raft-consensus-simulator
> 5. Raft Structure Advice：https://pdos.csail.mit.edu/6.824/labs/raft-structure.txt
> 6. Debugging by Pretty Printing：https://blog.josejg.com/debugging-pretty/
> 7. Students' Guide to Raft ：https://thesquareplanet.com/blog/students-guide-to-raft/
> 8. An Introduction to Distributed Systems**：**https://github.com/aphyr/distsys-class


## 0 一个 GET 操作的生命历程

<img src="https://guanghuihuang-1315055500.cos.ap-guangzhou.myqcloud.com/%E5%AD%A6%E4%B9%A0%E7%AC%94%E8%AE%B0/%E6%95%B0%E6%8D%AE%E5%BA%93/mit6.824/%E4%B8%80%E6%9D%A1Get%E6%93%8D%E4%BD%9C%E7%9A%84%E7%94%9F%E5%91%BD%E5%91%A8%E6%9C%9F.png" alt="image" style="zoom: 100%;" />

用户向 Client 客户端发送一个 Get 请求，Client 将其作为入参，通过 RPC 依次调用所有 Server 端的 Get 函数，直到调用到 Leader Server 为止

Leader Server 会将收到的 Get 请求作为一个 Get 操作日志传给 Raft 模块，Raft 模块会将其存放到 Log 日志数组的最新下标 n 处，然后 Leader Server 在结果管道 Map 中创建一个对应下标 n 的结果管道 ResultChannelN，然后监听

当 Leader Raft 通过一轮或多轮日志同步 RPC 后，有超过一半的 Raft 节点都同步到了这条 Get 操作日志，Leader Raft 更新 commitIndex 为 下标 n，并将日志添加到 Raft 模块的日志应用管道 AppChannel 中

Leader Server 的日志应用线程会监听 AppChannel，将新的操作日志应用到 KV 存储中，然后将查询结果添加到结果管道 ResultChannelN

Leader Server 主线程在 Get 函数监听到结果管道中的结果后，会将其作为 RPC 调用的结果返回给 Client


## 1 Raft 基本原理

### 使用场景

Raft 本质上是一种**共识算法**（consensus algorithm）。为什么要使用共识算法呢？什么场景下要使用共识算法呢？为什么共识算法逐渐成为了分布式系统的基石呢？

让我们来试着理一下内在逻辑：随着数字化的发展，当数据集（比如数据库中的一个 table）随着业务的增长，膨胀到一定地步之后，单机不再能存得下。因此需要将该数据集以某种方式切分成多个**分片**（Shard，也有地方称为 Partition、Region 等等）。分片之后，就可以将单个数据集分散到多个机器上。但是随着集群使用的机器数增多，整个集群范围内，**单个机器的故障**（各种软硬件、运维故障）概率就会增大。为了容忍机器故障、保证每个分片时时可用（可用性），我们通常会将分片**冗余多份**存在多个机器上，每个冗余称为一个**副本**（replication）。但如果分片持续有写入进来，从属于该分片的多个副本，由于机器故障、网络问题，就可能会出现数据不一致的问题。为了保证多副本间的数据一致性，我们引入了**共识算法**。

### 基本原理

Raft 会将**客户端请求**序列化成操作序列，称为**操作日志**（operation log），继而会确保 Raft 的所有**服务器**（Server）会看到相同的日志。其中，每个 **Raft** **Server** 是一个进程，在工程中会各自在独立机器上，但课程中为了方便测试，会将其在单机上使用多线程模拟。Raft Server，也可称为 **Raft Instance**、**Raft Peer**，都是说的一个概念。

在工程实践中，通常一个 Raft Server 会包含多个数据分片的**状态机**（对应上文说的数据分片的一个副本），不同机器上从属于一个数据分片的副本联合起来组成一个 Raft Group，这样一个集群中就会存在多个 Raft Group。如 TiKV 的架构（他们的数据分片叫 Region）：

<img src="https://guanghuihuang-1315055500.cos.ap-guangzhou.myqcloud.com/%E5%AD%A6%E4%B9%A0%E7%AC%94%E8%AE%B0/%E6%95%B0%E6%8D%AE%E5%BA%93/mit6.824/raft01.png" alt="image"  />

但在本课程 Raft 部分中，我们只考虑最简单情况：一个 Raft Server 中只包含一个状态机。在后面 ShardKV 部分中，会扩展到类似 TiKV 的架构。

所有 Raft Server 初始状态为空，然后会按照按相同的顺序执行相同的客户端请求，进而保证状态机是一致的。如果某个 Raft Server 宕机重启后，进度落下了，Raft 算法会负责将其日志进行追平。只要有**超过半数**的 Peer 存活并且可以互相通信，Raft 就可以继续正常运行。如果当前没有多数派存活，则 Raft 将会陷入停滞，不能继续接受客户端来的请求。不过，一旦多数 Peer 重新可以互通，Raft 就又可以继续工作。

这里面涉及几个核心概念角色：

1. **客户端（Client）**：Raft 的客户端，会向 Raft 发送写请求。
2. **服务器（Raft Server，也称为 Raft Instance 或 Raft Peer）**：组成 Raft 集群的单个进程。
3. **状态机（State Machine）**：每个 Raft Server 中会有一个本地的状态机。所谓状态机，在本课程里，可以理解为一个小型的 KV 存储。客户端的读取请求，都会直接打到状态机中。

三者关系可以参照[论文](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)中的图：

<img src="https://guanghuihuang-1315055500.cos.ap-guangzhou.myqcloud.com/%E5%AD%A6%E4%B9%A0%E7%AC%94%E8%AE%B0/%E6%95%B0%E6%8D%AE%E5%BA%93/mit6.824/raft02.png" alt="image" style="zoom: 25%;" />
图中的四个步骤含义如下：

1. **写入**：**客户端**向 Raft 发送写请求，写入数据
2. **同步**：Raft 模块将请求序列化为日志，然后：
   1. 写入 **Raft Server** 本地 log
   2. 同步到其他 Raft 实例
3. **应用**：每个实例会将 Raft Log 各自应用到**状态机**
4. **读取**：客户端向状态机发送读请求，读取之前写入（状态机中）的数据

## 2 Raft 实现框架

> 论文提供了一个工程级别的 Raft 实现，我们要保证准确实现了下图中的所有细节

<img src="https://guanghuihuang-1315055500.cos.ap-guangzhou.myqcloud.com/%E5%AD%A6%E4%B9%A0%E7%AC%94%E8%AE%B0/%E6%95%B0%E6%8D%AE%E5%BA%93/mit6.824/raft05.png" alt="image"  />

> 下面说一下我们如何来组织 Raft 代码，可以从三个角度来鸟瞰式地纵览 Raft：
>
> 1. 对外层服务提供接口
> 2. 三个工作流：领导选举、日志同步、日志应用
> 3. 一个状态机：raft角色状态机

### 数据结构

```go
type Raft struct {
      mu        sync.Mutex          // Lock to protect shared access to this peer's state
      peers     []*labrpc.ClientEnd // RPC end points of all peers
      persister *Persister          // Object to hold this peer's persisted state
      me        int                 // this peer's index into peers[]
      dead      int32               // set by Kill()

      // used for election loop
      role            Role
      currentTerm     int
      votedFor        int
      electionStart   time.Time
      electionTimeout time.Duration

      // log in Peer's local
      log []LogRecord

      // only used when it is Leader,
      // log view for each peer
      nextIndex  []int
      matchIndex []int

      // fields for apply loop
      commitIndex int
      lastApplied int
      applyCh     chan ApplyMsg
      applyCond   *sync.Cond
}
```

### 接口

在实现 Raft 时，主要需要对外提供以下接口

```Go
// 创建一个新的 Raft 实例
rf := Make(peers, me, persister, applyCh)

// 就一个新的日志条目达成一致
rf.Start(command interface{}) (index, term, isleader)

// 询问 Raft 实例其当前的 term，以及是否自认为 Leader（但实际由于网络分区的发生，可能并不是）
rf.GetState() (term, isLeader)

// 每次有日志条目提交时，每个 Raft 实例需要向该 channel 发送一条 apply 消息
type ApplyMsg struct
```

<img src="https://guanghuihuang-1315055500.cos.ap-guangzhou.myqcloud.com/%E5%AD%A6%E4%B9%A0%E7%AC%94%E8%AE%B0/%E6%95%B0%E6%8D%AE%E5%BA%93/mit6.824/raft03.png" alt="image" style="zoom: 100%;" />

服务通过调用 `Make(peers, me, persister, applyCh)` 创建一个 Raft Peer。其中 peers 是所有参与 Raft Group 的对端句柄数组，通过这些句柄，可以给所有 Peer 发送 RPC。`me` 是本 Raft Peer 在数组中的下标，测试会保证所有 Raft Peer 看到的数组中元素顺序是一致的。

`Start(command)` 是应用要求 Raft Group 提交一条日志，该函数是异步的，要立即返回，而无需等待日志提交后再返回。通过论文我们知道，只有 Leader 才能提交日志，因此如果不是 Leader 的 Peer 收到此调用后，立即返回说自己不是 Leader 即可。如果是 Leader 且最终提交成功，会通过 Make 时传入的 `applyCh` 将消息返回给应用层。

`GetState()` 是应用层用于确定当前 Raft Peer 的任期和状态。

### 三个工作流

所谓工作流，就是一个处理逻辑闭环，可以粗浅理解为一个循环（ loop），因此要分别独占一个线程。循环中通常要做一些事情，通常由两种方式触发：

1. **时钟触发（time-driven）**：周期性地做某件事，比如 Leader -> Follower 的发送日志：领导选举（leader election）、日志同步（ log replication）。
2. **事件触发（event-driven）**：条件满足时做某件事，比如日志应用（log application）。

每个工作流使用一个线程来实现，线程的最外层逻辑就是一个循环。循环当然总有个退出条件，对于 raft 来说，就是是否已经被 killed。

对于时钟触发（time-driven）的工作流，在 golang 中，我们至少有两种方式可以控制触发：

1.  `time.Sleep `
2. `time.Ticker` 和 `time.Timer`

方法 2 会涉及 **golang Channels** 和回调函数，使用起来相对复杂，不建议新手用。因此，我们选用方法 1，实现起来就很自然，比如发送一轮日志后，就调用 `time.Sleep ` 沉睡一个心跳周期。

对于事件触发（event-driven）的工作流，在 golang 中，我们也至少有两种方式可以控制：

1. `sync.Cond`
2. `Channel`

这里我们选用 `sync.Cond` ，因为可以配合锁使用，以保护一些使用共享变量的**临界区**。而 Channel 只能做简单的唤醒，并不能和锁配合使用。

### 状态机

每个 Peer 都会在三个角色：`Follower`，`Candidate` 和 `Leader` 间变换，当条件满足时，就会从一个角色变为另一个角色。

```go
type Role string
const (
   Follower  Role = "Follower"
   Candidate Role = "Candidate"
   Leader    Role = "Leader"
)
```

<img src="https://guanghuihuang-1315055500.cos.ap-guangzhou.myqcloud.com/%E5%AD%A6%E4%B9%A0%E7%AC%94%E8%AE%B0/%E6%95%B0%E6%8D%AE%E5%BA%93/mit6.824/raft04.png" alt="image" style="zoom: 33%;" />

可以根据这三个状态抽象出三个函数：`becomeFollower`，`becomeCandidate`，`becomeLeader`，即驱动状态机的“线”。并且增加一些限制，禁止一些图中不存在的“线”。其中 `Locked` 的后缀，说明该函数必须在锁的保护下调用，因为函数内部并没有加锁。另外，还可以加一些适当的日志，表明角色的转换过程，在进行调试时，角色转换是最重要的信息之一。

```Go
func (rf *Raft) becomeFollowerLocked(term int)
func (rf *Raft) becomeCandidateLocked()
func (rf *Raft) becomeLeaderLocked()
```

### MIT6.824的实现步骤

如果一次性实现 Raft 将会非常复杂，且很难一次性写对，因此我们将其分成四个部分来增量式的实现。Raft 最主要的逻辑就是领导者选举和日志同步，因此可以分别作为一个部分；之后，为了应对宕机重启，需要再实现 Raft 状态持久化和恢复逻辑；最后，随着写入的不断增多，日志会越来越大，照论文中的方案，我们会对之前的状态定时 snapshot，就可以将 snapshot 对应的老旧日志截断，只保留 snapshot 之后最新的日志。

简单来说，四个部分的分工大概是：

1. **Part A：领导者选举**：但不带日志比较
2. **Part B：日志同步**：同时完善领导选举，比较日志
3. **Part C：状态持久化**：将所有影响逻辑的状态外存
4. **Part D：日志压缩**：做 snapshot

每个部分通过测试后再进行下一部分，这种思想也是构建大型工程常用的：先跑通一个最小可用模型，然后通过测试保证正确后，再不断增量式迭代增加功能，每次都通过测试来保证增量部分的正确性。

## 3 领导选举

遵循 lab 中给出的命名方式，我们将所有的工作流称为 `xxxTicker` 。主要逻辑，就是一个大的 `for` 循环，隔一段时间 `time.Sleep(time.Duration(ms) * time.Millisecond)`就进行一次检查，看**选举时钟**否超时。如果超时，会转变为 Candidate，并发起选举。

所有用到 Raft 全局变量的地方都要加锁，但注意不要在加锁时进行**同步地**发送RPC。该 Loop 的生命周期和 Raft Peer 相同，即**在创建 Raft 实例时就在后台开始运行**。

```Go
func Make(peers []*labrpc.ClientEnd, me int,
        persister *Persister, applyCh chan ApplyMsg) *Raft {
  	...
        go rf.electionTicker()

        return rf
}
```

```Go
func (rf *Raft) electionTicker() {
        for !rf.killed() { // 选举 loop

                rf.mu.Lock()
                if rf.role != Leader && rf.isElectionTimeoutLocked() {
                        rf.becomeCandidateLocked()
                  go rf.startElection(rf.currentTerm) // 单轮选举 (包含单次 RPC)
                }
                rf.mu.Unlock()

                 // 选举 loop
                ms := 50 + (rand.Int63() % 300)
                time.Sleep(time.Duration(ms) * time.Millisecond)
        }
}
```

选举的过程，粗略来说，就是逐一向所有其他 Peer 发送“要票”（RequestVote） RPC，然后看是否能获得多数票以决定是否当选。RPC 耗时很不确定，因此不能**同步**地放到 `electionTicker` 中，每个 RPC 需要额外启动一个 goroutine 来执行。

我们以两个层次组织 RPC **发送方**（也就是 Candidate）**要票逻辑**和RPC **接受方**（其他 Peer）的**投票逻辑**：

1. **单轮选举**：超时成为 Candidate 之后，针对所有 Peer（除自己外）发起一次要票过程，我们称之为 `startElection`。
2. **单次 RPC**：针对每个 Peer 的 RequestVote 的请求和响应处理，由于要进行计票，需要用到一个局部变量 votes，因此我们使用一个`startElection` 中的嵌套函数来实现，称为 `askVoteFromPeer`。
3. **回调函数**：所有 Peer 在运行时都有可能收到要票请求，`RequestVote` 这个回调函数，就是定义该 Peer 收到要票请求的处理逻辑。

### 单轮选举

一轮选举包括针对除自己外所有 Peer 的一轮要票 RPC，由于需要访问全局变量，所以仍然要加锁。同样的，就不能在持有锁的时候，同步地进行 RPC。需要用 goroutine 异步地对每个 Peer 进行 RPC。

```Go
func (rf *Raft) startElection(term int) bool {
        votes := 0
        askVoteFromPeer := func(peer int, args *RequestVoteArgs) {
                // 单次 RPC
        }

        rf.mu.Lock()
        defer rf.mu.Unlock()

        // 检查上下文
        if rf.contextLostLocked(Candidate, term) {
                return false
        }

        for peer := 0; peer < len(rf.peers); peer++ {
                if peer == rf.me {
                        votes++
                        continue
                }

                args := &RequestVoteArgs{
                        Term:        term,
                        CandidateId: rf.me,
                }
                go askVoteFromPeer(peer, args, term)
        }

        return true
 }
```

**上下文检查**

这里面有个检查“上下文”是否丢失的关键函数：`contextLostLocked` **。上下文**，在不同的地方有不同的指代。在我们的 Raft 的实现中，“上下文”就是指 `Term` 和 `Role`。即在一个任期内，只要你的角色没有变化，就能放心地**推进状态机**。

```Go
func (rf *Raft) contextLostLocked(role Role, term int) bool {
        return !(rf.currentTerm == term && rf.role == role)
}
```

在多线程环境中，只有通过锁保护起来的**临界区**内的代码块才可以认为被原子地执行了。由于在 Raft 实现中，我们使用了大量的 goroutine，因此每当线程新进入一个临界区时，要进行 Raft 上下文的检查。如果 Raft 的上下文已经被更改，要及时终止 goroutine，避免对状态机做出错误的改动。

### 单次 RPC

单次 RPC 包括**构造 RPC 参数、发送 RPC等待结果、对 RPC 结果进行处理**三个部分。构造参数我们在 `startElection` 函数内完成了，因此 `askVoteFromPeer` 函数中就只需要包括后两个部分即可。（这里实现为嵌套函数的作用是，可以在各个 goroutine 中修改外层的局部变量 votes）

```Go
askVoteFromPeer := func(peer int, args *RequestVoteArgs, term int) {
        // send RPC
        reply := &RequestVoteReply{}
        ok := rf.sendRequestVote(peer, args, reply)

        // handle the response
        rf.mu.Lock()
        defer rf.mu.Unlock()
        if !ok {
                return
        }

        // align the term
        if reply.Term > rf.currentTerm {
                rf.becomeFollowerLocked(reply.Term)
                return
        }

        // 检查上下文
        if rf.contextLostLocked(Candidate, rf.currentTerm) {
                return
        }

        // count votes
        if reply.VoteGranted {
                votes++
        }
        if votes > len(rf.peers)/2 {
                rf.becomeLeaderLocked()
                go rf.replicationTicker(term)
        }
}
```

**对齐任期**

在接受到 RPC 或者处理 RPC 返回值时的第一步，就是要**对齐 Term**。因为 Term 在 Raft 中本质上是一种“优先级”或者“权力等级”的体现。**Peer 的 Term 相同，是对话展开的基础**，否则就要先对齐 Term：

1. **如果对方 Term 比自己小**：无视请求，通过返回值“亮出”自己的 Term
2. **如果对方 Term 比自己大**：乖乖跟上对方 Term，变成最“菜”的 Follower

对齐 Term 之后，还要检查上下文，即处理 RPC （RPC 回调函数也是在其他线程调用的）返回值和处理多线程本质上一样：都要首先确保**上下文**没有丢失，才能驱动状态机。

### 回调函数

回调函数实现的一个关键点，还是要先对齐 Term。不仅是因为这是之后展开“两个 Peer 对话”的基础，还是因为在对齐 Term 的过程中，Peer 有可能重置 `votedFor`。这样即使本来由于已经投过票了而不能再投票，但提高任期重置后，在新的 Term 里，就又有一票可以投了。

还有一点，论文里很隐晦地提到过：**只有投票给对方后，才能重置选举 timer**。换句话说，在没有投出票时，是不允许重置选举 timer 的。从**感性**上来理解，只有“**认可对方的权威**”（发现对方是 Leader 或者投票给对方）时，才会重置选举 timer —— 本质上是一种“**承诺**”：认可对方后，短时间就不再去发起选举争抢领导权。

```Go
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
        // Your code here (PartA, PartB).
        rf.mu.Lock()
        defer rf.mu.Unlock()

        // align the term
        reply.Term = rf.currentTerm
        if rf.currentTerm > args.Term {
                reply.VoteGranted = false
                return
        }
        if rf.currentTerm < args.Term {
                rf.becomeFollowerLocked(args.Term)
        }

        // check the votedFor
        if rf.votedFor != -1 {
                reply.VoteGranted = false
                return
        }

        reply.VoteGranted = true
        rf.votedFor = args.CandidateId
        rf.resetElectionTimerLocked()
}
```

至此，即便不实现基本的心跳RPC，PartA的测试也都能通过了。由于论文中心跳和日志同步共用了一个RPC，我们放到下一章。

## 4 日志同步

日志同步的工作流和领导选举很像，但又有所不同：

1. **生命周期不同**。领导选举的工作流会持续整个 raft 实例的存活周期；而日志同步地工作流只会持续它当选为领导的那个**任期**（ term）。之后如果再当选，会重新启动一个工作流。
2. **超时间隔不同**。领导选举的超时间隔是在一个小范围内随机的；而日志同步地超时间隔是固定的。

```Go
func (rf *Raft) replicationTicker(term int) {
        for !rf.killed() {
                contextRemained := rf.startReplication(term)
                if !contextRemained {
                        break
                }

                time.Sleep(replicateInterval)
        }
}
```

需要说明的是：在论文中**心跳**（不带日志）和**日志复制**是用的一个 RPC，毕竟他们在逻辑上唯一的区别就是带不带日志。但在工程实践中，为了提升性能，有的 Raft 实现会将其进行分开。我们的课程里，为了保持简洁，也就实现到一块了。因为对于初学者来说，**简洁比性能重要**。

心跳 Loop 和和选举 Loop 逻辑相对，我们也分三个层次来实现 RPC 发送方和 RPC 接收方：

1. **单轮心跳**：对除自己外的所有 Peer 发送一个心跳 RPC，称为 `startReplication`
2. **单次 RPC**：对某个 Peer 来发送心跳，并且处理 RPC 返回值，称为 `replicateToPeer`
3. **回调函数**：所有 Peer 在运行时都有可能收到要票请求，`RequestVote` 这个回调函数，就是定义该 Peer 收到要票请求的处理逻辑

PartB 需要定义日志格式，在心跳逻辑的基础上，需要日志同步的逻辑。总体上来说，Leader 需要维护一个各个 Peer 的进度视图（`nextIndex` 和 `matchIndex` 数组）。其中 `nextIndex` 用于进行**日志同步时**的**匹配点试探**，`matchIndex` 用于**日志同步成功后**的**匹配点记录**。依据全局匹配点分布，我们可以计算出当前全局的 `commitIndex`，然后再通过之后轮次的日志复制 RPC 下发给各个 Follower。

每个 Follower 收到 `commitIndex` 之后，再去 apply 本地的已提交日志到状态机。但这个 **apply 的流程**，我们留到之后一章来专门做，本章就暂时留待 TODO 了。因此，本章就只实现逻辑：**匹配点的试探**和**匹配后的更新**。

由于不用构造**随机**超时间隔，心跳 Loop 会比选举 Loop 简单很多：

```Go
func (rf *Raft) replicationTicker(term int) {
        for !rf.killed() {
                contextRemained := rf.startReplication(term)
                if !contextRemained {
                        break
                }

                time.Sleep(replicateInterval)
        }
}
```

与选举 Loop 不同的是，这里的 `startReplication` 有个返回值，主要是检测“上下文”是否还在（ `ContextLost` ）——一旦发现 Raft Peer 已经不是这个 term 的 Leader 了，就立即退出 Loop。所以日志同步地工作流只会持续它当选为领导的那个**任期**（ term）

### 单轮心跳

和 Candidate 的选举逻辑类似，Leader 会给除自己外的所有其他 Peer 发送心跳。在发送前要检测“上下文”是否还在，如果不在了，就直接返回 false ——告诉外层循环 `replicationTicker` 可以终止循环了。因此 `startReplication` 的返回值含义为：是否成功的发起了一轮心跳。

```Go
func (rf *Raft) startReplication(term int) bool {
  			rf.mu.Lock()
        defer rf.mu.Unlock()

        // 检查上下文
				if rf.contextLostLocked(Leader, term) {
          			return false
        }

        for peer := 0; peer < len(rf.peers); peer++ {
                if peer == rf.me {
                        // Leader 自己的 matchIndex 也顺便更新一下，方便后面更新 commitIndex
                        rf.matchIndex[peer] = len(rf.log) - 1
                        rf.nextIndex[peer] = len(rf.log)
                        continue
                }

                // 日志同步相关参数构造
                prevIdx := rf.nextIndex[peer] - 1
                prevTerm := rf.log[prevIdx].Term
                args := &AppendEntriesArgs{
                        Term:         rf.currentTerm,
                        LeaderId:     rf.me,
                        PrevLogIndex: prevIdx,
                        PrevLogTerm:  prevTerm,
                        Entries:      rf.log[prevIdx+1:],
                        LeaderCommit: rf.commitIndex,
                }
                go replicateToPeer(peer, args, term)
        }

        return true
}
```

### 单次 RPC

在不关心日志时，心跳的返回值处理比较简单，只需要对齐下 term 就行。

对于日志同步，需要增加一致性检查结果处理的两部分逻辑：

1. 一致性检查失败：
   - 当一致性检查失败，说明 Leader 相应 peer 在 nextIndex 前一个位置的任期不一致 （leader.log[idx].Term > peer.log[idx].Term）
   - 则需要将匹配点回退，继续试探。可以回退一格或者加速试探：往前找到 Leader.log 在上一个 Term 的最后一个 index
2. 如果复制成功：
   - 当一致性检查成功，说明找到了 Leader 和 Follower 达成一致的最大索引条目，Follower 删除了从这一点之后的所有条目，并同步复制了 Leader 从那一点之后的条目
   - Leader 则需要更新为该 Follower 维护的 nextIndex 和 matchIndex，表示 Leader 要发送给 Follower 的下一个日志条目的索引
   - 还需看看是否可以更新 Leader 的 commitIndex （也留到之后实现，TODO，补充小节链接）

```Go
func (rf *Raft) replicateToPeer(peer int, args *AppendEntriesArgs, term int) {
        reply := &AppendEntriesReply{}
        ok := rf.sendAppendEntries(peer, args, reply)

        rf.mu.Lock()
        defer rf.mu.Unlock()
        if !ok {
              	return
        }

        // 检查上下文
        if rf.contextLostLocked(Leader, term) {
          LOG(rf.me, rf.currentTerm, DLog, "-> S%d, Context Lost, T%d:Leader->T%d:%s", peer, term, rf.currentTerm, rf.role)
          return
        }

        if reply.Term > rf.currentTerm {
                rf.becomeFollowerLocked(reply.Term)
                return
        }

        // 一致性检查失败：
        if !reply.Success {
                idx := rf.nextIndex[peer] - 1
                term := rf.log[idx].Term
                for idx > 0 && rf.log[idx].Term == term {
                  idx--
                }
                rf.nextIndex[peer] = idx + 1
                return
        }

        // 一致性检查成功：
        rf.matchIndex[peer] = args.PrevLogIndex + len(args.Entries)
        rf.nextIndex[peer] = rf.matchIndex[peer] + 1

        // TODO: 是否可以更新 Leader 的 commitIndex 
}
```

这部分的最终目的，就是要更新 `matchIndex`。进而依据所有 Peer 的 `matchIndex` 来算 `commitIndex` 。Leader 有了 `commitIndex` 之后，再将其下发给各个 Follower，指导其各自更新本地 `commitIndex` 进而 apply。

但细心的你可能会注意到一件事，`matchIndex` 和 `nextIndex`  是什么时候初始化的？所以，我们要继续补上这两个字段的初始化逻辑。本质上来说，这两个字段是各个 Peer 中日志进度在 Leader 中的一个**视图**（view）。Leader 正是依据此视图来决定给各个 Peer 发送多少日志。也是依据此视图，Leader 可以计算全局的 `commitIndex`。因此，该视图只在 Leader 当选的 Term 中有用。故而，我们要在 Leader 一当选时，更新该视图，即 `becomeLeaderLocked` 中。

```Go
func (rf *Raft) becomeLeaderLocked() {
        if rf.role != Candidate {
                return
        }

        rf.role = Leader
        for peer := 0; peer < len(rf.peers); peer++ {
                rf.nextIndex[peer] = len(rf.log)
                rf.matchIndex[peer] = 0
        }
}
```

### 回调函数

心跳逻辑：心跳接收方在收到心跳时，只要 Leader 的 term 不小于自己，就对其进行认可，变为 Follower，并重置选举时钟，承诺一段时间内不发起选举。

对于日志同步，需要增加一致性检查结果处理的两部分逻辑：

1. 一致性检查失败：如果 `prevLog` 不匹配，则返回 `Success = False`
2. 一致性检查成功：如果 `prevLog` 匹配，则将参数中的 `Entries` 追加到本地日志，返回 `Success = True`。

所谓一致性检查：就是**相同 Index 的地方，Term 相同**；即 index 和 term 能唯一确定一条日志，这是因为，Raft 算法保证一个 Term 中最多有（也可能没有）一个 Leader，然后只有该 Leader 能确定日志顺序且同步日志。这样一来，Term 单调递增，每个 Term 只有一个 Leader，则该 Leader 能唯一确定该 Term 内的日志顺序。

```Go
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
        rf.mu.Lock()
        defer rf.mu.Unlock()

        reply.Term = rf.currentTerm
        reply.Success = false

        if args.Term < rf.currentTerm {
          return
        }
        if args.Term >= rf.currentTerm {
          rf.becomeFollowerLocked(args.Term)
        }

        // 一致性检查
        if args.PrevLogIndex >= len(rf.log) {
          return
        }
        if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
          return
        }

        // 日志同步
        rf.log = append(rf.log[:args.PrevLogIndex+1], args.Entries...)
        reply.Success = true

        // TODO: 处理 Leader 的 commitIndex 

        rf.resetElectionTimerLocked()
}
```

### 选举日志比较

Raft 通过实现一个**安全性限制**来保证日志的一致性：**对于给定任期号，Leader 都包含了之前各个任期所有被提交的日志条目**

为此需要实现一个**选举限制**：保证新的 Leader 在当选时就包含了之前所有任期号中已经提交的日志条目，为此需要进行**日志比较**：确保只有具有比大多数 Peer 的日志更新的候选人才能当选 Leader。比较对象是最后一个 LogRecord，**比较规则**是：

1. Term 高者更新
2. Term 相同，Index 大者更新

```Go
func (rf *Raft) isMoreUpToDateLocked(candidateIndex, candidateTerm int) bool {
        l := len(rf.log)
        lastTerm, lastIndex := rf.log[l-1].Term, l-1

        if lastTerm != candidateTerm {
                return lastTerm > candidateTerm
        }
        return lastIndex > candidateIndex
}
```

选举这一块需要增加的逻辑比较简单，只需要：

1. 在发送 RPC 构造参数时增加上最后一条日志信息
2. 在接收 RPC 投票前比较日志新旧

```Go
type RequestVoteArgs struct {
        Term         int
        CandidateId  int
        LastLogIndex int
        LastLogTerm  int
}
```

发送方（Candidate），在发送 RPC 增加构造参数，带上 Candidate 最后一条日志的信息（index 和 term）。

```Go
func (rf *Raft) startElection(term int) {
  	// ...
        args := &RequestVoteArgs{
                Term:         rf.currentTerm,
                CandidateId:  rf.me,
                LastLogIndex: len(rf.log) - 1,
                LastLogTerm:  rf.log[len(rf.log)-1].Term,
        }

        go askVoteFromPeer(peer, args)
  	// ...
}
```

接收方（各个 Peer 的回调函数），在对齐 Term，检查完没有投过票之后，进一步比较最后一条日志，看谁的更新。如果本 Peer 比 Candidate 更新，则拒绝投票给 Candidate。

```Go
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
        rf.mu.Lock()
        defer rf.mu.Unlock()

        reply.Term = rf.currentTerm
        reply.VoteGranted = false
  
        if args.Term < rf.currentTerm {
                return
        }
        if args.Term > rf.currentTerm {
                rf.becomeFollowerLocked(args.Term)
        }

        if rf.votedFor != -1 {
                return
        }

        // 日志比较
        if rf.isMoreUpToDateLocked(args.LastLogTerm, args.LastLogIndex) {
                return
        }

        reply.VoteGranted = true
        rf.votedFor = args.CandidateId
        rf.resetElectionTimerLocked()
}
```

## 5 日志应用

由于日志应用只有在 `commitIndex` 变大的时候才会触发，因此我们可以使用 golang 中的条件变量  `sync.Cond`，使用唤醒机制，只有在 `commitIndex` 增大后才唤醒 `applyTicker`。只要理解了 `sync.Cond` 的使用，日志应用的工作流是比较简单的：当条件满足时，遍历相关日志，逐个 apply。由于 apply 需要将 msg 逐个送到 channel 中，而 channel 耗时是不确定的，需要放到临界区外，不要在加锁的情况下进行，因此可以提前取出需要 apply 的日志

于是，我们把 apply 分为三个阶段：

1. 构造所有待 apply 的 `ApplyMsg`
2. 遍历这些 msgs，进行 apply
3. 更新 `lastApplied`

```Go
func (rf *Raft) applicationTicker() {
        for !rf.killed() {
                rf.mu.Lock()
                rf.applyCond.Wait()
          			
                entries := make([]LogEntry, 0)
                for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
                        entries = append(entries, rf.log[i])
                }
                rf.mu.Unlock()

                for i, entry := range entries {
                        rf.applyCh <- ApplyMsg {
                                CommandValid: entry.CommandValid,
                                Command:      entry.Command,
                                CommandIndex: rf.lastApplied + 1 + i, // must be cautious
                        }
                }

                rf.mu.Lock()
                rf.lastApplied += len(entries)
                rf.mu.Unlock()
        }
}
```

只要我们保证全局就只有这一个 apply 的地方，那我们这样分成三个部分问题就不大。尤其是需要注意，当后面增加  snapshot  apply 的逻辑时，也要放到该函数里。

需要针对日志应用逻辑给 Raft 数据结构补上几个字段：

1. `commitIndex`：全局日志提交进度
2. `lastApplied`：本 Peer 日志 apply 进度
3. `applyCond`：使用 `sync.Cond` 唤醒日志应用的工作流
4. `applyCh`：apply 的过程，就是将 applyMsg 通过构造 Peer 时传进来的 channel 返回给应用层

增加`applyCond`初始化：**applyCond** 在初始化时，需要关联到一把锁上，这是` sync.Cond` 的使用要求，之后只有在该锁临界区内才可以进行 `Wait()` 和 `Signal()` 的调用。对于我们来说，这把锁自然就是全局的那把大锁：`rf.mu`

```Go
func Make(peers []*labrpc.ClientEnd, me int,
        persister *Persister, applyCh chan ApplyMsg) *Raft {
        // ......
        
        rf.applyCh = applyCh
        rf.commitIndex = 0
        rf.lastApplied = 0
        rf.applyCond = sync.NewCond(&rf.mu)

        // initialize from state persisted before a crash
        rf.readPersist(persister.ReadRaftState())

        // start ticker goroutine to start elections
        go rf.electionTicker()
        go rf.applicationTicker()

        return rf
}
```

### Leader CommitIndex 更新

在日志同步的一致性检查成功后，会更新 `rf.matchIndex`。在每次更新 `rf.matchIndex` 后，依据此全局匹配点视图，我们可以算出多数 Peer 的匹配点，进而更新 Leader 的 `CommitIndex`。如果 `commitIndex` 更新后，则唤醒 apply 工作流，提醒可以 apply 新的日志到本地了。

```Go
func (rf *Raft) replicateToPeer(peer int, args *AppendEntriesArgs) {
        // ......

				// 一致性检查成功，更新 rf.matchIndex 后
        majorityMatched := rf.getMajorityIndexLocked()
        if majorityMatched > rf.commitIndex {
                rf.commitIndex = majorityMatched
                rf.applyCond.Signal()
        }
}
```

我们可以使用**排序后找中位数**的方法来计算，由于排序会改变原数组，因此要把 matchIndex 复制一份再进行排序

```Go
func (rf *Raft) getMajorityIndexLocked() int {
        // TODO(spw): may could be avoid copying
        tmpIndexes := make([]int, len(rf.matchIndex))
        copy(tmpIndexes, rf.matchIndex)
        sort.Ints(sort.IntSlice(tmpIndexes))
        majorityIdx := (len(tmpIndexes) - 1) / 2
        return tmpIndexes[majorityIdx] // min -> max
}
```

### Follower CommitIndex 更新

在 Leader CommitIndex 更新后，会通过下一次的 `AppendEntries` 的 RPC 参数发送给每个 Follower。则首先，在 `AppendEntriesArgs` 中增加 `LeaderCommit` 参数。每个 Follower 通过 AppendEntries 的回调函数收到 Leader 发来的 `LeaderCommit`，来更新本地的 `CommitIndex`，进而驱动 Apply 工作流开始干活

```Go
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
        // ...

        // TODO: handle the args.LeaderCommit
        if args.LeaderCommit > rf.commitIndex {
          rf.commitIndex = args.LeaderCommit
          rf.applyCond.Signal()
        }

        // reset the election timer, promising not start election in some interval
        rf.resetElectionTimerLocked()
}
```

### Context 检查总结

需要 Context 检查的主要有四个地方：

1. startReplication 前，检查自己仍然是给定 term 的 Leader
2. replicateToPeer 处理 reply 时，检查自己仍然是给定 term 的 Leader
3. startElection 前，检查自己仍然是给定 term 的 Candidate
4. askVoteFromPeer 处理 reply 时，检查自己仍然是给定 term 的 Candidate

## 6 状态持久化

略

## 7 日志压缩（快照）

日志压缩后，Raft需要记录额外的两个信息，`lastIncludeIndex`、`lastIncludeTerm`表示快照中最后一个log的index和Term。



## 8 分布式 KV

本文将基于前面实现的 raft 算法，构建一个高可用的分布式 Key/Value 服务

<img src="https://guanghuihuang-1315055500.cos.ap-guangzhou.myqcloud.com/%E5%AD%A6%E4%B9%A0%E7%AC%94%E8%AE%B0/%E6%95%B0%E6%8D%AE%E5%BA%93/mit6.824/1280X1280.PNG" alt="image"  />

大致的流程是客户端向后端的 servers 发起请求，后端的服务是由多个节点组成的，每个节点之间使用 raft 进行状态复制，客户端会选择将请求发送到 Leader 节点，然后由 Leader 节点进行状态复制，即发送日志，当收到多数的节点成功提交日志的响应之后，Leader 会更新自己的 commitIndex，表示这条日志提交成功，并且 apply 到状态机中，然后返回结果给客户端。

在这一个分布式 KV 部分完成之后，加上前面已经实现了的 raft 部分，我们就基本实现了下图中提到的每一个部分（这个图可能大家在前面的学习中已经看到过了，主要包含了 raft 的一些主要方法的状态转换和基于 raft 的 KV 服务的交互）

<img src="https://guanghuihuang-1315055500.cos.ap-guangzhou.myqcloud.com/%E5%AD%A6%E4%B9%A0%E7%AC%94%E8%AE%B0/%E6%95%B0%E6%8D%AE%E5%BA%93/mit6.824/1280X1280%20%281%29.PNG" alt="image"  />

在 mit6.824 课程中，分布式 KV 部分主要是在目录 kvraft 中，大致包含客户端和服务端的逻辑

客户端是由一个叫 Clerk 的结构体进行表示的，它主要是维护了客户端发送请求到后端 KV 服务的逻辑

```Go
type Clerk struct {
   servers []*labrpc.ClientEnd
   // You will have to modify this struct.
}
```

Clerk 会向 KV 服务发送三种类型的请求：
- Get：通过 key 获取 value
- Put：设置 key/value 对
- Append：将值追加到 key 对应的 value 上，如果 key 不存在，则相当于 Put

需要注意的是我们的 Get/Put/Append 方法需要保证是具有线性一致性的。线性一致性简单来说是要求客户端的修改对后续的请求都是生效的，即其他客户端能够立即看到修改后的结果，而不会因为我们的多副本机制而看到不一样的结果，最终的目的是要我们的 kv 服务对客户端来说看起来“像是只有一个副本”

服务端的代码主要是在 server.go 中，结构体 KVServer 描述了一个后端的 kv server 节点所维护的状态：

```Go
type KVServer struct {
   mu      sync.Mutex
   me      int
   rf      *raft.Raft
   applyCh chan raft.ApplyMsg
   dead    int32 // set by Kill()

   maxraftstate int // snapshot if log grows this big

   // Your definitions here.
}
```

可以看到 KVServer 维护了一个 raft 库中的 Raft 结构体，表示其是一个 raft 集群中的节点，它会通过 raft 提供的功能向其他的 KVServer 同步状态，让整个 raft 集群中的数据保持一致

### 1 kvraft Client 端处理

客户端的结构体 Clerk：

```Go
type Clerk struct {
   servers []*labrpc.ClientEnd
   // You will have to modify this struct.
}
```

可以看到其中维护了 servers 列表，表示的是后端分布式 KV 服务的所有节点信息，我们可以通过这个信息去向指定的节点发送数据读写的请求

每个 kv 服务的节点都是一个 raft 的 peer，客户端发送请求到 kv 服务的 Leader 节点，然后 Leader 节点会存储请求日志在本地，然后将日志通过 raft 发送给其他的节点进行状态同步。所以 raft 日志其实存储的是一连串客户端请求，然后 server 节点会按照顺序执行请求，并将结果存储到状态机中

Get 方法的处理大致如下：

```Go
func (ck *Clerk) Get(key string) string {
   args := GetArgs{Key: key}
   for {
      var reply GetReply
      ok := ck.servers[ck.leaderId].Call("KVServer.Get", &args, &reply)
      if !ok || reply.Err == ErrWrongLeader || reply.Err == ErrTimeout {
         // 节点id加一，继续重试
         ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
         continue
      }
      // 请求成功，返回 value
      return reply.Value
   }
}
```

Put 和 Append 的逻辑由于比较类似，所以将其作为一个 RPC 请求，只是加了一个名为 Op 的参数加以区分。

```Go
func (ck *Clerk) PutAppend(key string, value string, op string) {
   args := PutAppendArgs{
      Key:   key,
      Value: value,
      Op:    op,
   }

   for {
      var reply PutAppendReply
      ok := ck.servers[ck.leaderId].Call("KVServer.PutAppend", &args, &reply)
      if !ok || reply.Err == ErrWrongLeader || reply.Err == ErrTimeout {
         // 节点id加一，继续重试
         ck.leaderId = (ck.leaderId + 1) % len(ck.servers)
         continue
      }
      // 请求成功
      return
   }
}
```

### 2 kvraft Server 端处理

按照我们前面梳理的大致交互逻辑，客户端的请求到达之后，我们需要首先通过 raft 模块将其存储到 raft 日志中，回想一下我们在前面实现的 raft 库中，提供了一个 `Start` 入口方法，这个方法是 raft 接收外部请求的，我们会将请求通过这个方法传递过去

```Go
func (rf *Raft) Start(command interface{}) (int, int, bool) {
   rf.mu.Lock()
   defer rf.mu.Unlock()

   if rf.role != Leader {
      return 0, 0, false
   }
   rf.log = append(rf.log, LogEntry{
      CommandValid: true,
      Command:      command,
      Term:         rf.currentTerm,
   })
   LOG(rf.me, rf.currentTerm, DLeader, "Leader accept log [%d]T%d", len(rf.log)-1, rf.currentTerm)
   rf.persistLocked()

   return len(rf.log) - 1, rf.currentTerm, true
}
```

`Start` 方法会返回当前的节点是不是 Leader，如果不是的话，我们需要向客户端反馈一个 `ErrWrongLeader` 错误，客户端获取到之后，发现此节点并不是 Leader，那么会挑选下一个节点重试请求

当 raft 集群中的 Leader 节点处理了 Start 请求之后，它会向其他的 Follower 节点发送 `AppendEntries` 消息，将日志同步到其他的 Follower 节点，当大多数的节点都正常处理之后，Leader 会收到消息，然后更新自己的 `commitIndex`，然后将日志通过 applyCh 这个通道发送过去

<img src="https://guanghuihuang-1315055500.cos.ap-guangzhou.myqcloud.com/%E5%AD%A6%E4%B9%A0%E7%AC%94%E8%AE%B0/%E6%95%B0%E6%8D%AE%E5%BA%93/mit6.824/raft02.png" alt="image" style="zoom: 25%;" />

然后 kv server 收到 apply 消息之后，将命令应用到本地的状态机中，然后返回给客户端

也就是说，我们客户端需要一直等待 raft 模块同步完成，并且 Leader 节点将操作应用到状态机之后，才会返回结果给客户端

所以这里我们需要启动一个后台线程来执行 apply 任务，主要是从 applyCh 中接收来自 raft 的日志消息，处理完之后，将结果存储到一个 channel 中，这时候 Get/Put/Append 方法就能从这个 channel 中获取到结果，并返回给客户端

```Go
type KVServer struct {
   mu      sync.Mutex
   me      int
   rf      *raft.Raft
   applyCh chan raft.ApplyMsg
   dead    int32 // set by Kill()

   maxraftstate int // snapshot if log grows this big

   // Your definitions here.
}
```
