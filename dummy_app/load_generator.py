import time
import random
import requests

# Simple load generator you can run from host or inside the container.
# It will repeatedly call /burst and toggle /chaos to force alerts.

BASE = 'http://127.0.0.1:8000'


def run(loop=0, burst_every=60, burst_size=50, chaos_every=300, chaos_duration=60):
    i = 0
    next_burst = time.time() + 5
    next_chaos = time.time() + chaos_every
    while loop == 0 or i < loop:
        now = time.time()
        if now >= next_burst:
            try:
                print(f'Triggering burst size={burst_size}')
                requests.post(f'{BASE}/burst', json={'count': burst_size, 'delay': 0.01}, timeout=5)
            except Exception as e:
                print('burst failed', e)
            next_burst = now + burst_every
        if now >= next_chaos:
            try:
                print('Entering chaos mode')
                requests.post(f'{BASE}/chaos', json={'mode': 'error'}, timeout=5)
            except Exception as e:
                print('start chaos failed', e)
            time.sleep(chaos_duration)
            try:
                print('Restoring normal mode')
                requests.post(f'{BASE}/chaos', json={'mode': 'normal'}, timeout=5)
            except Exception as e:
                print('stop chaos failed', e)
            next_chaos = now + chaos_every
        time.sleep(1)
        i += 1


if __name__ == '__main__':
    run(loop=0, burst_every=60, burst_size=100, chaos_every=300, chaos_duration=60)
