// versions: v0.8.0..; outcome: ok
x: {if: 1, try: 2, func: 3, else: 4, "a-b": 5}
y: x.if + x.try + x.func + x.else + x."a-b"
