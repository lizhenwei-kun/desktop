"""
ACP (Agent Client Protocol) 客户端
连接到 codebuddy --acp 服务端，通过 stdin/stdout 走 JSON-RPC 2.0 通信
纯 Python 实现，零依赖
"""

import asyncio
import json
import sys
import uuid


class ACPClient:
    """
    ACP 协议客户端，通过子进程 stdin/stdout 通信

    使用方式:
        async with ACPClient() as client:
            await client.initialize()
            session = await client.new_session()
            await client.prompt(session, "写个 hello world")
    """

    def __init__(self, command="codebuddy", args=None):
        self._command = command
        self._args = args or []
        self._process = None
        self._reader = None
        self._writer = None
        self._req_id = 0
        self._pending = {}

    async def __aenter__(self):
        await self._start()
        return self

    async def __aexit__(self, *args):
        await self.close()

    async def _start(self):
        """启动子进程并连接 stdin/stdout"""
        import os as _os
        import sys as _sys
        cmd = '"{}" --acp {}'.format(self._command, " ".join(self._args))
        self._process = await asyncio.create_subprocess_shell(
            cmd,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        self._writer = self._process.stdin
        self._reader = self._process.stdout
        print(f"[ACP] 已连接: {' '.join(cmd)} (PID={self._process.pid})")

    def _next_id(self):
        self._req_id += 1
        return str(self._req_id)

    async def _send(self, method, params=None, msg_id=None):
        """发送 JSON-RPC 请求"""
        msg = {
            "jsonrpc": "2.0",
            "id": msg_id or self._next_id(),
            "method": method,
            "params": params or {},
        }
        line = json.dumps(msg, ensure_ascii=False)
        print(f">>> [{method}]")
        self._writer.write((line + "\n").encode("utf-8"))
        await self._writer.drain()
        return msg["id"]

    async def _notify(self, method, params=None):
        """发送 JSON-RPC 通知（无需响应）"""
        msg = {
            "jsonrpc": "2.0",
            "method": method,
            "params": params or {},
        }
        line = json.dumps(msg, ensure_ascii=False)
        print(f">>> [通知] {method}")
        self._writer.write((line + "\n").encode("utf-8"))
        await self._writer.drain()

    async def _recv(self):
        """读取一行 JSON-RPC 消息"""
        line = await self._reader.readline()
        if not line:
            raise ConnectionError("Agent 进程已关闭")
        return json.loads(line.decode("utf-8").strip())

    async def _request(self, method, params=None):
        """发送请求并等待响应"""
        req_id = await self._send(method, params)
        while True:
            msg = await self._recv()
            # 通知类消息直接跳过
            if "method" in msg:
                await self._handle_notification(msg)
                continue
            # 找到匹配的响应
            if msg.get("id") == req_id:
                if "error" in msg:
                    raise Exception(f"请求失败 [{method}]: {msg['error']}")
                return msg.get("result", {})

    async def _handle_notification(self, msg):
        """处理服务端推送的通知"""
        method = msg.get("method", "")
        params = msg.get("params", {})
        if method == "session/update":
            content = params.get("content", "")
            if content:
                print(content, end="", flush=True)
        elif method == "session/request_permission":
            # 自动批准权限请求
            req_id = params.get("requestId", str(uuid.uuid4()))
            print(f"\n[自动批准] {params.get('title', '')}")
            await self._send(
                "session/approve",
                {"requestId": req_id},
                msg_id=req_id,
            )

    # ====== ACP 标准方法 ======

    async def initialize(self):
        """Step 1: 初始化，协商协议版本和能力"""
        result = await self._request("initialize", {
            "protocolVersion": 1,
            "capabilities": {},
            "clientInfo": {
                "name": "acp-python-client",
                "version": "1.0.0",
            },
        })
        # 初始化后发送 initialized 通知
        await self._notify("initialized")
        print(f"[初始化完成] Agent: {result.get('agentInfo', {}).get('name', 'unknown')}")
        print(f"[调试] initialize 返回: {json.dumps(result, indent=2, ensure_ascii=False)}")

        # 检查是否需要认证
        auth_methods = result.get("authMethods", [])
        if auth_methods:
            # 优先用 token 方式认证（服务端会使用本地缓存的 token）
            token_method = next((m for m in auth_methods if m["id"] == "token"), auth_methods[0])
            print(f"[认证] 使用 {token_method['name']} 方式...")
            auth_result = await self._request("authenticate", {
                "method": token_method["id"],
            })
            print(f"[认证完成]")
            result = auth_result

        return result

    async def new_session(self, cwd=None):
        """Step 2: 创建新会话"""
        result = await self._request("session/new", {
            "sessionId": str(uuid.uuid4()),
            "cwd": cwd or ".",
            "mcpServers": [],
        })
        session_id = result.get("sessionId", "")
        print(f"[会话已创建] {session_id}")
        return session_id

    async def prompt(self, session_id, message):
        """Step 3: 发送提示词，流式接收响应"""
        print(f"\n[发送提示词] {message}")
        req_id = await self._send("session/prompt", {
            "message": {
                "role": "user",
                "content": message,
            },
        })

        full_text = ""
        while True:
            msg = await self._recv()
            # 处理流式通知
            if msg.get("method") == "session/update":
                params = msg.get("params", {})
                content = params.get("content", "")
                if content:
                    full_text += content
                    print(content, end="", flush=True)
                continue

            # 处理权限请求
            if msg.get("method") == "session/request_permission":
                params = msg.get("params", {})
                req_id_perm = params.get("requestId", str(uuid.uuid4()))
                print(f"\n[自动批准] {params.get('title', '')}")
                await self._send(
                    "session/approve",
                    {"requestId": req_id_perm},
                    msg_id=req_id_perm,
                )
                continue

            # session/prompt 的最终响应
            if msg.get("id") == req_id:
                result = msg.get("result", {})
                stop_reason = result.get("stopReason", "")
                print(f"\n\n[完成] 停止原因: {stop_reason}")
                return full_text, result

    async def close(self):
        """关闭连接"""
        if self._process and self._process.returncode is None:
            self._process.terminate()
            try:
                await asyncio.wait_for(self._process.wait(), timeout=5)
            except asyncio.TimeoutError:
                self._process.kill()
            print("[ACP] 已断开")


async def main():
    prompt = " ".join(sys.argv[1:]) or "用 python 写一个 hello world 程序"

    async with ACPClient() as client:
        await client.initialize()
        session = await client.new_session()
        text, result = await client.prompt(session, prompt)


if __name__ == "__main__":
    asyncio.run(main())
