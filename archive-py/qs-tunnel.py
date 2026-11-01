#!/usr/bin/env python3
"""qs-tunnel.py: SMB-over-QUIC tunnel (aioquic, real QUIC/UDP transport).

Same topology/ports as quicsmb (C version):
  client: listen TCP (default 127.0.0.1:8445) -> QUIC/UDP -> server
  server: QUIC/UDP (default :4433) -> TCP backend SMB (default 127.0.0.1:4455)

Transport is REAL QUIC (TLS1.3 + UDP, ALPN=quicsmb/1). One TCP connection =
one QUIC connection, single bidi stream (id 0), bytes transparent.
Multiplexing N streams per connection is a later optimization; correctness first.

Usage:
  server: python3 qs-tunnel.py server --quic-port 4433 --backend-host 127.0.0.1 --backend-port 4455 --cert cert.pem --key key.pem
  client: python3 qs-tunnel.py client --server-host <tailscale-ip> --server-port 4433 --listen-host 127.0.0.1 --listen-port 8445
"""
import argparse
import asyncio
import logging
import ssl

from aioquic.asyncio import connect, serve
from aioquic.asyncio.protocol import QuicConnectionProtocol
from aioquic.quic.configuration import QuicConfiguration
from aioquic.quic.events import QuicEvent, StreamDataReceived, ConnectionTerminated

ALPN = "quicsmb/1"
STREAM_ID = 0  # single bidi stream per QUIC connection
BUF = 65536

log = logging.getLogger("qs-tunnel")


class ForwardProtocol(QuicConnectionProtocol):
    """Base: QUIC stream 0 <-> TCP writer. Subclass wires TCP peer."""

    def __init__(self, *args, tcp_writer=None, **kwargs):
        super().__init__(*args, **kwargs)
        self._tcp_writer = tcp_writer
        self._closed = asyncio.Event()
        # Backpressure-safe transmit: each QUIC connection has its own
        # asyncio.DatagramTransport; transmit() just flushes its queue.
        # NOTE: must only be called from the event-loop thread. Our pumps
        # all run on the same loop, but StreamDataReceived fires inside
        # datagram_received -> also the loop thread. OK.

    def send_to_quic(self, data: bytes):
        try:
            self._quic.send_stream_data(STREAM_ID, data, end_stream=False)
        except Exception as e:
            log.warning("send_stream_data failed: %s", e)
            return
        try:
            self.transmit()
        except Exception as e:
            log.warning("transmit failed: %s", e)

    def quic_event_received(self, event: QuicEvent):
        if isinstance(event, StreamDataReceived):
            if self._tcp_writer and event.data:
                self._tcp_writer.write(event.data)
                # Drain in background so the QUIC callback never blocks
                # while the TCP peer is slow; pump keeps ordering.
                asyncio.ensure_future(self._drain_tcp())
        elif isinstance(event, ConnectionTerminated):
            self._closed.set()
            if self._tcp_writer:
                try:
                    self._tcp_writer.close()
                except Exception:
                    pass

    async def _drain_tcp(self):
        try:
            await self._tcp_writer.drain()
        except Exception:
            pass


async def tcp_to_quic(reader: asyncio.StreamReader, proto: ForwardProtocol, tag: str):
    try:
        while True:
            data = await reader.read(BUF)
            if not data:
                break
            proto.send_to_quic(data)
    except (asyncio.CancelledError, ConnectionResetError, BrokenPipeError):
        pass
    except Exception as e:
        log.warning("%s tcp->quic: %s", tag, e)
    finally:
        try:
            proto._quic.send_stream_data(STREAM_ID, b"", end_stream=True)
            proto.transmit()
        except Exception:
            pass


# ---------------- client ----------------
async def handle_tcp_client(tcp_reader, tcp_writer, cfg):
    peer = tcp_writer.get_extra_info("peername")
    tag = f"cli-{peer}"
    log.info("%s tcp accepted", tag)
    tls_cfg = QuicConfiguration(is_client=True, alpn_protocols=[ALPN])
    tls_cfg.verify_mode = ssl.CERT_NONE  # dev: self-signed; TODO: pin cert
    try:
        async with connect(
            cfg.server_host, cfg.server_port, configuration=tls_cfg,
            create_protocol=lambda *a, **k: ForwardProtocol(*a, tcp_writer=tcp_writer, **k),
        ) as proto:
            pump = asyncio.create_task(tcp_to_quic(tcp_reader, proto, tag))
            await proto._closed.wait()
            pump.cancel()
    except Exception as e:
        log.warning("%s quic dial failed: %s", tag, e)
    finally:
        try:
            tcp_writer.close()
        except Exception:
            pass
    log.info("%s done", tag)


async def run_client(cfg):
    server = await asyncio.start_server(
        lambda r, w: handle_tcp_client(r, w, cfg), cfg.listen_host, cfg.listen_port)
    log.info("client: tcp %s:%d -> quic %s:%d (ALPN=%s)",
             cfg.listen_host, cfg.listen_port, cfg.server_host, cfg.server_port, ALPN)
    async with server:
        await server.serve_forever()


# ---------------- server ----------------
class ServerProtocol(ForwardProtocol):
    def _flush_pending(self):
        pend = getattr(self, "_pending", None)
        if pend and self._tcp_writer:
            self._tcp_writer.write(bytes(pend))
            pend.clear()
            asyncio.ensure_future(self._drain_tcp())

    def __init__(self, *args, backend_host=None, backend_port=445, **kwargs):
        super().__init__(*args, **kwargs)
        self._backend_host = backend_host
        self._backend_port = backend_port
        self._backend_ready = asyncio.Event()
        self._connecting = False
        self._pending = bytearray()

    def quic_event_received(self, event: QuicEvent):
        if isinstance(event, StreamDataReceived):
            # aioquic coalesces + may deliver end_stream-only events;
            # buffer until backend is up, then flush in order.
            if event.data:
                if not hasattr(self, "_pending"):
                    self._pending = bytearray()
                self._pending += event.data
            if self._tcp_writer:
                self._flush_pending()
            elif not self._connecting:
                self._connecting = True
                asyncio.create_task(self._dial_backend(b""))
            if event.end_stream and self._tcp_writer:
                try:
                    self._tcp_writer.write_eof()
                except Exception:
                    pass
        elif isinstance(event, ConnectionTerminated):
            self._closed.set()
            if self._tcp_writer:
                try:
                    self._tcp_writer.close()
                except Exception:
                    pass

    async def _dial_backend(self, first_data: bytes):
        try:
            r, w = await asyncio.open_connection(self._backend_host, self._backend_port)
            self._tcp_writer = w
            if first_data:
                w.write(first_data)
                await w.drain()
            self._backend_ready.set()
            self._flush_pending()
            asyncio.create_task(self._backend_pump(r))
            log.info("server: stream -> backend %s:%d",
                     self._backend_host, self._backend_port)
        except Exception as e:
            log.warning("server: backend dial failed: %s", e)
            self._quic.close()
            self.transmit()

    async def _backend_pump(self, reader):
        try:
            while True:
                data = await reader.read(BUF)
                if not data:
                    log.info("server: backend EOF, closing QUIC")
                    break
                self.send_to_quic(data)
        except Exception as e:
            log.warning("server backend->quic: %s", e)
        finally:
            # Backend gone: fully close QUIC connection; client will see
            # ConnectionTerminated and close its TCP side. (No half-close:
            # impacket answers then waits, half-close would hang the client.)
            try:
                self._quic.close()
                self.transmit()
            except Exception:
                pass


async def run_server(cfg):
    tls_cfg = QuicConfiguration(is_client=False, alpn_protocols=[ALPN])
    tls_cfg.load_cert_chain(cfg.cert, cfg.key)
    # Each inbound connection gets its own protocol instance; factory needs
    # the backend target bound per-connection.
    bound = {"host": cfg.backend_host, "port": cfg.backend_port}
    def factory(*a, **k):
        return ServerProtocol(*a, backend_host=bound["host"],
                              backend_port=bound["port"], **k)
    log.info("server: quic :%d -> smb %s:%d (ALPN=%s)",
             cfg.quic_port, cfg.backend_host, cfg.backend_port, ALPN)
    server = await serve("0.0.0.0", cfg.quic_port, configuration=tls_cfg,
                         create_protocol=factory)
    # QuicServer runs on the event loop; park forever (cancel to stop).
    await asyncio.get_running_loop().create_future()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("mode", choices=["client", "server"])
    ap.add_argument("--server-host", default="127.0.0.1")
    ap.add_argument("--server-port", type=int, default=4433)
    ap.add_argument("--listen-host", default="127.0.0.1")
    ap.add_argument("--listen-port", type=int, default=8445)
    ap.add_argument("--quic-port", type=int, default=4433)
    ap.add_argument("--backend-host", default="127.0.0.1")
    ap.add_argument("--backend-port", type=int, default=4455)
    ap.add_argument("--cert", default="cert.pem")
    ap.add_argument("--key", default="key.pem")
    ap.add_argument("-v", "--verbose", action="store_true")
    cfg = ap.parse_args()
    logging.basicConfig(level=logging.DEBUG if cfg.verbose else logging.INFO,
                        format="%(asctime)s %(levelname)s %(message)s")
    if cfg.mode == "client":
        asyncio.run(run_client(cfg))
    else:
        asyncio.run(run_server(cfg))


if __name__ == "__main__":
    main()
