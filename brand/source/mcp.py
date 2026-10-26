import json, subprocess, sys, os
S=os.path.dirname(os.path.abspath(__file__))
B=os.path.dirname(S)  # brand/: sources and exports
ROOTS=[B]+[r for r in os.environ.get("VC_EXTRA_ROOTS","").split(":") if r]
class MCP:
    def __init__(self):
        self.p=subprocess.Popen(["vectorcraft-cli","mcp","--headless"]+sum([["--automation-read-root",r,"--automation-write-root",r] for r in ROOTS],[]),
            stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True)
        self.i=0
        r=self.req("initialize",{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude","version":"1"}})
        self.note("notifications/initialized")
    def note(self,m,p=None):
        self.p.stdin.write(json.dumps({"jsonrpc":"2.0","method":m,**({"params":p} if p else {})})+"\n"); self.p.stdin.flush()
    def req(self,m,p=None):
        self.i+=1
        self.p.stdin.write(json.dumps({"jsonrpc":"2.0","id":self.i,"method":m,"params":p or {}})+"\n"); self.p.stdin.flush()
        while True:
            line=self.p.stdout.readline()
            if not line: raise SystemExit("server closed")
            msg=json.loads(line)
            if msg.get("id")==self.i: return msg
    def call(self,name,args):
        r=self.req("tools/call",{"name":name,"arguments":args})
        if "error" in r: return {"error":r["error"]}
        return r["result"]
