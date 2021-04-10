import asyncio
import concurrent.futures
from contextlib import contextmanager
from threading import Thread


@contextmanager
def background_thread_loop():
    def run_forever(loop):
        asyncio.set_event_loop(loop)
        loop.run_forever()

    _loop = asyncio.new_event_loop()

    _thread = Thread(target=run_forever, args=(_loop, ), daemon=True)
    _thread.start()
    yield _loop


def run_async(coro, loop, timeout=None):
    fut = asyncio.run_coroutine_threadsafe(coro, loop)
    try:
        return fut.result(timeout), True
    except concurrent.futures.TimeoutError:
        return None, False