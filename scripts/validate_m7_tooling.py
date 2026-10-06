#!/usr/bin/env python3
import sqlite3
from pathlib import Path

root = Path(__file__).resolve().parents[1]
sql = (root / "migrations" / "0001_initial.sql").read_text()
con = sqlite3.connect(":memory:")
con.execute("PRAGMA foreign_keys=ON")
con.executescript(sql)
now = 1_800_000_000_000
con.execute("INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Test','active',1,?,?)", (now,now))
con.execute("INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,?,?)", (now,now))
con.execute("INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','agent','active',?,?)", (now,now))
con.execute("""INSERT INTO tool_invocations(
 id,workspace_id,principal_id,tool_id,tool_version,adapter_id,adapter_version,mode,resource_ref,input_hash,status,created_at,updated_at
) VALUES('inv','ws','agent','synthetic.echo','1','builtin.synthetic','1','read','synthetic://demo','sha256:x','created',?,?)""", (now,now))
con.execute("UPDATE tool_invocations SET status='authorized',updated_at=? WHERE id='inv' AND status='created'", (now+1,))
con.execute("UPDATE tool_invocations SET status='running',started_at=?,updated_at=? WHERE id='inv' AND status='authorized'", (now+2,now+2))
con.execute("UPDATE tool_invocations SET status='succeeded',result_json='{}',ended_at=?,updated_at=? WHERE id='inv' AND status='running'", (now+3,now+3))
row = con.execute("SELECT status,mode,result_json FROM tool_invocations WHERE id='inv'").fetchone()
assert row == ('succeeded','read','{}'), row

for column, bad in [('mode','root_shell'), ('status','denied')]:
    try:
        con.execute(f"UPDATE tool_invocations SET {column}=? WHERE id='inv'", (bad,))
        raise AssertionError(f"invalid {column} accepted")
    except sqlite3.IntegrityError:
        pass

print("M7 tool_invocations schema lifecycle: PASS")
