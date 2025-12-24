"""Initialize the SQLite DB used by the dummy app."""
import sqlite3
from datetime import datetime

DB = "./dummy.db"

def init_db(path=DB):
    conn = sqlite3.connect(path)
    cur = conn.cursor()
    cur.execute(
        "CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, created_at TEXT)"
    )
    now = datetime.utcnow().isoformat()
    sample = [(f"item-{i}", now) for i in range(1, 6)]
    cur.executemany("INSERT INTO items (name, created_at) VALUES (?, ?)", sample)
    conn.commit()
    conn.close()

if __name__ == "__main__":
    init_db()
    print("Initialized DB with sample rows at ./dummy.db")
