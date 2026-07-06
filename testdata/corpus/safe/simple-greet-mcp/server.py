"""Minimal MCP server: returns 'Hello, <name>!'"""

def greet(params):
    name = params.get('name', 'world')
    if not isinstance(name, str):
        raise TypeError('name must be a string')
    return {'message': f'Hello, {name}!'}

TOOLS = {
    'greet': greet,
}
