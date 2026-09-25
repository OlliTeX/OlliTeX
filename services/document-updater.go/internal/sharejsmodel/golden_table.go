// Code generated from the /tmp/modgen oracle (model.js). DO NOT EDIT BY HAND.
// Regenerate: NODE_PATH=/tmp/modgen/node_modules node /tmp/modgen/gen.js
package sharejsmodel

// goldenJSON is the oracle-pinned model log for the 12 scenarios
// (every model-level emit, every public-call callback row, and the
// final DB-side counters).
const goldenJSON = `{
 "basic": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 0,
   "snap": "Xa",
   "old": "a",
   "metaSrc": "s1"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 0
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 1,
   "snap": "Xa"
  },
  {
   "c": "getVersion",
   "err": null,
   "v": 1
  },
  {
   "c": "getOps",
   "err": null,
   "ops": [
    {
     "op": [
      {
       "p": 0,
       "i": "X"
      }
     ],
     "v": 0,
     "src": "s1"
    }
   ]
  },
  {
   "c": "getOps",
   "err": null,
   "ops": [
    {
     "op": [
      {
       "p": 0,
       "i": "X"
      }
     ],
     "v": 0,
     "src": "s1"
    }
   ]
  },
  {
   "c": "getOps",
   "err": null,
   "ops": []
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 1,
   "getOps": 1,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 0,
     "end": null
    }
   ],
   "writeOps": [
    0
   ],
   "writeSnaps": [],
   "delete": []
  }
 ],
 "transform": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 1,
   "snap": "a"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 1,
   "snap": "a"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 1,
   "snap": "aY",
   "old": "a"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 1
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 2,
   "snap": "aY"
  },
  {
   "c": "applyOp",
   "err": "Delete component 'z' does not match deleted text 'a'"
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 1,
   "getOps": 1,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 1,
     "end": null
    }
   ],
   "writeOps": [
    1
   ],
   "writeSnaps": [],
   "delete": []
  }
 ],
 "errors": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 3,
   "snap": "ab"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 3,
   "snap": "ab"
  },
  {
   "c": "applyOp",
   "err": "Op at future version"
  },
  {
   "c": "applyOp",
   "err": "undefined is not iterable (cannot read property Symbol(Symbol.iterator))"
  },
  {
   "c": "applyOp",
   "err": "Op already submitted"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 3,
   "snap": "Zab",
   "old": "ab"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 3
  },
  {
   "c": "getVersion",
   "err": null,
   "v": 4
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 1,
   "getOps": 3,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 3,
     "end": null
    },
    {
     "key": "a:1",
     "start": 0,
     "end": 3
    },
    {
     "key": "a:1",
     "start": 0,
     "end": 3
    }
   ],
   "writeOps": [
    3
   ],
   "writeSnaps": [],
   "delete": []
  }
 ],
 "maxdoc": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 1,
   "snap": "abc"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 1,
   "snap": "abc"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 1,
   "snap": "defgabc",
   "old": "abc"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 1
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 2,
   "snap": "defgabc"
  },
  {
   "c": "applyOp",
   "err": "Update takes doc over max doc size"
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 2,
   "snap": "defgabc"
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 1,
   "getOps": 1,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 1,
     "end": null
    }
   ],
   "writeOps": [
    1
   ],
   "writeSnaps": [],
   "delete": []
  }
 ],
 "opscache": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 1,
   "snap": "b"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 1,
   "snap": "b"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 1,
   "snap": "cb",
   "old": "b"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 1
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 2,
   "snap": "dcb",
   "old": "cb"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 2
  },
  {
   "c": "getOps",
   "err": null,
   "ops": [
    {
     "op": [
      {
       "p": 0,
       "i": "b"
      }
     ],
     "v": 0
    },
    {
     "op": [
      {
       "p": 0,
       "i": "c"
      }
     ],
     "v": 1
    },
    {
     "op": [
      {
       "p": 0,
       "i": "d"
      }
     ],
     "v": 2
    }
   ]
  },
  {
   "c": "getOps",
   "err": null,
   "ops": [
    {
     "op": [
      {
       "p": 0,
       "i": "c"
      }
     ],
     "v": 1
    },
    {
     "op": [
      {
       "p": 0,
       "i": "d"
      }
     ],
     "v": 2
    }
   ]
  },
  {
   "c": "getOps",
   "err": null,
   "ops": []
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 1,
   "getOps": 2,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 1,
     "end": null
    },
    {
     "key": "a:1",
     "start": 0,
     "end": 3
    }
   ],
   "writeOps": [
    1,
    2
   ],
   "writeSnaps": [],
   "delete": []
  }
 ],
 "listen": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 2,
   "snap": "cd"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 2,
   "snap": "cd"
  },
  {
   "c": "listen",
   "err": null
  },
  {
   "ev": "doc-op",
   "l": 0,
   "v": 0
  },
  {
   "ev": "doc-op",
   "l": 0,
   "v": 1
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 2,
   "snap": "ecd",
   "old": "cd"
  },
  {
   "ev": "doc-op",
   "l": 0,
   "v": 2
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 2
  },
  {
   "c": "removeListener",
   "doc": "a:1"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 3,
   "snap": "fecd",
   "old": "ecd"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 3
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 1,
   "getOps": 2,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 2,
     "end": null
    },
    {
     "key": "a:1",
     "start": 0,
     "end": 2
    }
   ],
   "writeOps": [
    2,
    3
   ],
   "writeSnaps": [],
   "delete": []
  }
 ],
 "coalesce": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "c": "getVersion",
   "err": null,
   "v": 0
  },
  {
   "c": "getVersion",
   "err": null,
   "v": 0
  },
  {
   "c": "getVersion",
   "err": null,
   "v": 0
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 0,
   "snap": "a"
  },
  {
   "c": "getSnapshot",
   "err": "not-in-db"
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 2,
   "getOps": 1,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 0,
     "end": null
    }
   ],
   "writeOps": [],
   "writeSnaps": [],
   "delete": []
  }
 ],
 "snaps": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 0,
   "snap": "ba",
   "old": "a"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 0
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 1,
   "snap": "cba",
   "old": "ba"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 1
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 2,
   "snap": "cba"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 2,
   "snap": "dcba",
   "old": "cba"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 2
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 3,
   "snap": "dcba"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 3,
   "snap": "edcba",
   "old": "dcba"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 3
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 1,
   "getOps": 1,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 0,
     "end": null
    }
   ],
   "writeOps": [
    0,
    1,
    2,
    3
   ],
   "writeSnaps": [
    2,
    4
   ],
   "delete": []
  }
 ],
 "metaop": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "c": "listen",
   "err": null
  },
  {
   "ev": "doc-op",
   "l": 0
  },
  {
   "ev": "model-applyMetaOp",
   "name": "a:1",
   "path": [
    "shout"
   ],
   "value": "v"
  },
  {
   "c": "applyMetaOp",
   "err": null,
   "v": 0
  },
  {
   "c": "applyMetaOp",
   "err": "path should be an array"
  },
  {
   "c": "applyMetaOp",
   "err": "not-in-db"
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 2,
   "getOps": 1,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 0,
     "end": null
    }
   ],
   "writeOps": [],
   "writeSnaps": [],
   "delete": []
  }
 ],
 "createdelete": [
  {
   "ev": "model-add",
   "name": "c:1",
   "v": 0,
   "snap": ""
  },
  {
   "ev": "model-create",
   "name": "c:1",
   "v": 0,
   "snap": "",
   "type": "text"
  },
  {
   "c": "create"
  },
  {
   "c": "getVersion",
   "err": null,
   "v": 0
  },
  {
   "c": "create",
   "err": "Invalid document name"
  },
  {
   "c": "create",
   "err": "Document already exists"
  },
  {
   "c": "create",
   "err": "Type not found"
  },
  {
   "ev": "model-applyOp",
   "name": "c:1",
   "opV": 0,
   "snap": "Q",
   "old": ""
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 0
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 1,
   "snap": "Q"
  },
  {
   "ev": "model-delete",
   "name": "c:1"
  },
  {
   "c": "delete"
  },
  {
   "ev": "model-load",
   "name": "c:1",
   "v": 1,
   "snap": "Q"
  },
  {
   "ev": "model-add",
   "name": "c:1",
   "v": 1,
   "snap": "Q"
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 1,
   "snap": "Q"
  },
  {
   "ev": "model-delete",
   "name": "zz:0"
  },
  {
   "c": "delete"
  },
  {
   "t": "db-final",
   "create": [
    "c:1"
   ],
   "getSnap": 1,
   "getOps": 1,
   "getOpsCalls": [
    {
     "key": "c:1",
     "start": 0,
     "end": null
    }
   ],
   "writeOps": [
    0
   ],
   "writeSnaps": [],
   "delete": [
    "c:1",
    "zz:0"
   ]
  }
 ],
 "nodeb": [
  {
   "ev": "model-add",
   "name": "n:1",
   "v": 0,
   "snap": ""
  },
  {
   "ev": "model-create",
   "name": "n:1",
   "v": 0,
   "snap": "",
   "type": "text"
  },
  {
   "c": "create"
  },
  {
   "c": "getVersion",
   "err": null,
   "v": 0
  },
  {
   "ev": "model-applyOp",
   "name": "n:1",
   "opV": 0,
   "snap": "Q",
   "old": ""
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 0
  },
  {
   "c": "getSnapshot",
   "err": null,
   "v": 1,
   "snap": "Q"
  },
  {
   "c": "getOps",
   "err": "Document does not exist"
  },
  {
   "c": "getVersion",
   "err": "Document does not exist"
  },
  {
   "c": "delete",
   "err": "Document does not exist"
  }
 ],
 "cloisedb": [
  {
   "ev": "model-load",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "ev": "model-add",
   "name": "a:1",
   "v": 0,
   "snap": "a"
  },
  {
   "ev": "model-applyOp",
   "name": "a:1",
   "opV": 0,
   "snap": "ba",
   "old": "a"
  },
  {
   "c": "applyOp",
   "err": null,
   "v": 0
  },
  {
   "t": "db-final",
   "create": [],
   "getSnap": 1,
   "getOps": 1,
   "getOpsCalls": [
    {
     "key": "a:1",
     "start": 0,
     "end": null
    }
   ],
   "writeOps": [
    0
   ],
   "writeSnaps": [],
   "delete": []
  }
 ]
}`
