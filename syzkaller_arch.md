Entrance: syz/sys-manager.go

- manager creation: &Manager
- rpc server started to communicate with Fuzzer: startRPCServer(), listenAndServe()
    - handle connection:
        - handshake: 
            - recv: Connection Request Raw
            - send: Connection Reply
            - recv: InfoRequest Raw
            - send: InfoReply
            - RunCheck()
                - machinechecked()
                    - NewFuzzer(): 
                        - create &Fuzzer in Manager
                        - newExecQueues(): register callback function genFuzz(), genFuzz() will generate or mutate the existing test cases or generate new test cases based on current programs and choiceTable
                        genFuzz() may call: mutateProgRequest(), genProgRequest(), which calls fuzzer.ChoiceTable() in  to make choice table, prepare to register the triage process for req. 
                        - **Thread**: choiceTableUpdater()
                            - choiceTableUpdater() listen to Fuzzer.ctRegenerate channel, when the channel is set by fuzzer.ChoiceTable()
            - ConnectionLoop()
- run instances, run fuzzer on instance 
- VMLoop()


Entrance syz/sys-fuzzer.go

- getTarget()
- send: ConnectionRequest
- recv ConnectionReplyRaw
- send: InfoRequest
- recv: InfoReplyRaw
- create FuzzerTool
- startProc()
- handleConnection()


