/******/ (() => { // webpackBootstrap
/******/ 	"use strict";
/******/ 	var __webpack_modules__ = ({

/***/ "node:path"
(module) {

module.exports = require("node:path");

/***/ },

/***/ "node:worker_threads"
(module) {

module.exports = require("node:worker_threads");

/***/ },

/***/ "../../.yarn/cache/@lezer-common-npm-1.5.0-321d54f8ca-12c4b0ea9d.zip/node_modules/@lezer/common/dist/index.js"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   DefaultBufferLength: () => (/* binding */ DefaultBufferLength),
/* harmony export */   IterMode: () => (/* binding */ IterMode),
/* harmony export */   MountedTree: () => (/* binding */ MountedTree),
/* harmony export */   NodeProp: () => (/* binding */ NodeProp),
/* harmony export */   NodeSet: () => (/* binding */ NodeSet),
/* harmony export */   NodeType: () => (/* binding */ NodeType),
/* harmony export */   NodeWeakMap: () => (/* binding */ NodeWeakMap),
/* harmony export */   Parser: () => (/* binding */ Parser),
/* harmony export */   Tree: () => (/* binding */ Tree),
/* harmony export */   TreeBuffer: () => (/* binding */ TreeBuffer),
/* harmony export */   TreeCursor: () => (/* binding */ TreeCursor),
/* harmony export */   TreeFragment: () => (/* binding */ TreeFragment),
/* harmony export */   parseMixed: () => (/* binding */ parseMixed)
/* harmony export */ });
/**
The default maximum length of a `TreeBuffer` node.
*/
const DefaultBufferLength = 1024;
let nextPropID = 0;
class Range {
    constructor(from, to) {
        this.from = from;
        this.to = to;
    }
}
/**
Each [node type](#common.NodeType) or [individual tree](#common.Tree)
can have metadata associated with it in props. Instances of this
class represent prop names.
*/
class NodeProp {
    /**
    Create a new node prop type.
    */
    constructor(config = {}) {
        this.id = nextPropID++;
        this.perNode = !!config.perNode;
        this.deserialize = config.deserialize || (() => {
            throw new Error("This node type doesn't define a deserialize function");
        });
        this.combine = config.combine || null;
    }
    /**
    This is meant to be used with
    [`NodeSet.extend`](#common.NodeSet.extend) or
    [`LRParser.configure`](#lr.ParserConfig.props) to compute
    prop values for each node type in the set. Takes a [match
    object](#common.NodeType^match) or function that returns undefined
    if the node type doesn't get this prop, and the prop's value if
    it does.
    */
    add(match) {
        if (this.perNode)
            throw new RangeError("Can't add per-node props to node types");
        if (typeof match != "function")
            match = NodeType.match(match);
        return (type) => {
            let result = match(type);
            return result === undefined ? null : [this, result];
        };
    }
}
/**
Prop that is used to describe matching delimiters. For opening
delimiters, this holds an array of node names (written as a
space-separated string when declaring this prop in a grammar)
for the node types of closing delimiters that match it.
*/
NodeProp.closedBy = new NodeProp({ deserialize: str => str.split(" ") });
/**
The inverse of [`closedBy`](#common.NodeProp^closedBy). This is
attached to closing delimiters, holding an array of node names
of types of matching opening delimiters.
*/
NodeProp.openedBy = new NodeProp({ deserialize: str => str.split(" ") });
/**
Used to assign node types to groups (for example, all node
types that represent an expression could be tagged with an
`"Expression"` group).
*/
NodeProp.group = new NodeProp({ deserialize: str => str.split(" ") });
/**
Attached to nodes to indicate these should be
[displayed](https://codemirror.net/docs/ref/#language.syntaxTree)
in a bidirectional text isolate, so that direction-neutral
characters on their sides don't incorrectly get associated with
surrounding text. You'll generally want to set this for nodes
that contain arbitrary text, like strings and comments, and for
nodes that appear _inside_ arbitrary text, like HTML tags. When
not given a value, in a grammar declaration, defaults to
`"auto"`.
*/
NodeProp.isolate = new NodeProp({ deserialize: value => {
        if (value && value != "rtl" && value != "ltr" && value != "auto")
            throw new RangeError("Invalid value for isolate: " + value);
        return value || "auto";
    } });
/**
The hash of the [context](#lr.ContextTracker.constructor)
that the node was parsed in, if any. Used to limit reuse of
contextual nodes.
*/
NodeProp.contextHash = new NodeProp({ perNode: true });
/**
The distance beyond the end of the node that the tokenizer
looked ahead for any of the tokens inside the node. (The LR
parser only stores this when it is larger than 25, for
efficiency reasons.)
*/
NodeProp.lookAhead = new NodeProp({ perNode: true });
/**
This per-node prop is used to replace a given node, or part of a
node, with another tree. This is useful to include trees from
different languages in mixed-language parsers.
*/
NodeProp.mounted = new NodeProp({ perNode: true });
/**
A mounted tree, which can be [stored](#common.NodeProp^mounted) on
a tree node to indicate that parts of its content are
represented by another tree.
*/
class MountedTree {
    constructor(
    /**
    The inner tree.
    */
    tree, 
    /**
    If this is null, this tree replaces the entire node (it will
    be included in the regular iteration instead of its host
    node). If not, only the given ranges are considered to be
    covered by this tree. This is used for trees that are mixed in
    a way that isn't strictly hierarchical. Such mounted trees are
    only entered by [`resolveInner`](#common.Tree.resolveInner)
    and [`enter`](#common.SyntaxNode.enter).
    */
    overlay, 
    /**
    The parser used to create this subtree.
    */
    parser, 
    /**
    [Indicates](#common.IterMode.EnterBracketed) that the nested
    content is delineated with some kind
    of bracket token.
    */
    bracketed = false) {
        this.tree = tree;
        this.overlay = overlay;
        this.parser = parser;
        this.bracketed = bracketed;
    }
    /**
    @internal
    */
    static get(tree) {
        return tree && tree.props && tree.props[NodeProp.mounted.id];
    }
}
const noProps = Object.create(null);
/**
Each node in a syntax tree has a node type associated with it.
*/
class NodeType {
    /**
    @internal
    */
    constructor(
    /**
    The name of the node type. Not necessarily unique, but if the
    grammar was written properly, different node types with the
    same name within a node set should play the same semantic
    role.
    */
    name, 
    /**
    @internal
    */
    props, 
    /**
    The id of this node in its set. Corresponds to the term ids
    used in the parser.
    */
    id, 
    /**
    @internal
    */
    flags = 0) {
        this.name = name;
        this.props = props;
        this.id = id;
        this.flags = flags;
    }
    /**
    Define a node type.
    */
    static define(spec) {
        let props = spec.props && spec.props.length ? Object.create(null) : noProps;
        let flags = (spec.top ? 1 /* NodeFlag.Top */ : 0) | (spec.skipped ? 2 /* NodeFlag.Skipped */ : 0) |
            (spec.error ? 4 /* NodeFlag.Error */ : 0) | (spec.name == null ? 8 /* NodeFlag.Anonymous */ : 0);
        let type = new NodeType(spec.name || "", props, spec.id, flags);
        if (spec.props)
            for (let src of spec.props) {
                if (!Array.isArray(src))
                    src = src(type);
                if (src) {
                    if (src[0].perNode)
                        throw new RangeError("Can't store a per-node prop on a node type");
                    props[src[0].id] = src[1];
                }
            }
        return type;
    }
    /**
    Retrieves a node prop for this type. Will return `undefined` if
    the prop isn't present on this node.
    */
    prop(prop) { return this.props[prop.id]; }
    /**
    True when this is the top node of a grammar.
    */
    get isTop() { return (this.flags & 1 /* NodeFlag.Top */) > 0; }
    /**
    True when this node is produced by a skip rule.
    */
    get isSkipped() { return (this.flags & 2 /* NodeFlag.Skipped */) > 0; }
    /**
    Indicates whether this is an error node.
    */
    get isError() { return (this.flags & 4 /* NodeFlag.Error */) > 0; }
    /**
    When true, this node type doesn't correspond to a user-declared
    named node, for example because it is used to cache repetition.
    */
    get isAnonymous() { return (this.flags & 8 /* NodeFlag.Anonymous */) > 0; }
    /**
    Returns true when this node's name or one of its
    [groups](#common.NodeProp^group) matches the given string.
    */
    is(name) {
        if (typeof name == 'string') {
            if (this.name == name)
                return true;
            let group = this.prop(NodeProp.group);
            return group ? group.indexOf(name) > -1 : false;
        }
        return this.id == name;
    }
    /**
    Create a function from node types to arbitrary values by
    specifying an object whose property names are node or
    [group](#common.NodeProp^group) names. Often useful with
    [`NodeProp.add`](#common.NodeProp.add). You can put multiple
    names, separated by spaces, in a single property name to map
    multiple node names to a single value.
    */
    static match(map) {
        let direct = Object.create(null);
        for (let prop in map)
            for (let name of prop.split(" "))
                direct[name] = map[prop];
        return (node) => {
            for (let groups = node.prop(NodeProp.group), i = -1; i < (groups ? groups.length : 0); i++) {
                let found = direct[i < 0 ? node.name : groups[i]];
                if (found)
                    return found;
            }
        };
    }
}
/**
An empty dummy node type to use when no actual type is available.
*/
NodeType.none = new NodeType("", Object.create(null), 0, 8 /* NodeFlag.Anonymous */);
/**
A node set holds a collection of node types. It is used to
compactly represent trees by storing their type ids, rather than a
full pointer to the type object, in a numeric array. Each parser
[has](#lr.LRParser.nodeSet) a node set, and [tree
buffers](#common.TreeBuffer) can only store collections of nodes
from the same set. A set can have a maximum of 2**16 (65536) node
types in it, so that the ids fit into 16-bit typed array slots.
*/
class NodeSet {
    /**
    Create a set with the given types. The `id` property of each
    type should correspond to its position within the array.
    */
    constructor(
    /**
    The node types in this set, by id.
    */
    types) {
        this.types = types;
        for (let i = 0; i < types.length; i++)
            if (types[i].id != i)
                throw new RangeError("Node type ids should correspond to array positions when creating a node set");
    }
    /**
    Create a copy of this set with some node properties added. The
    arguments to this method can be created with
    [`NodeProp.add`](#common.NodeProp.add).
    */
    extend(...props) {
        let newTypes = [];
        for (let type of this.types) {
            let newProps = null;
            for (let source of props) {
                let add = source(type);
                if (add) {
                    if (!newProps)
                        newProps = Object.assign({}, type.props);
                    let value = add[1], prop = add[0];
                    if (prop.combine && prop.id in newProps)
                        value = prop.combine(newProps[prop.id], value);
                    newProps[prop.id] = value;
                }
            }
            newTypes.push(newProps ? new NodeType(type.name, newProps, type.id, type.flags) : type);
        }
        return new NodeSet(newTypes);
    }
}
const CachedNode = new WeakMap(), CachedInnerNode = new WeakMap();
/**
Options that control iteration. Can be combined with the `|`
operator to enable multiple ones.
*/
var IterMode;
(function (IterMode) {
    /**
    When enabled, iteration will only visit [`Tree`](#common.Tree)
    objects, not nodes packed into
    [`TreeBuffer`](#common.TreeBuffer)s.
    */
    IterMode[IterMode["ExcludeBuffers"] = 1] = "ExcludeBuffers";
    /**
    Enable this to make iteration include anonymous nodes (such as
    the nodes that wrap repeated grammar constructs into a balanced
    tree).
    */
    IterMode[IterMode["IncludeAnonymous"] = 2] = "IncludeAnonymous";
    /**
    By default, regular [mounted](#common.NodeProp^mounted) nodes
    replace their base node in iteration. Enable this to ignore them
    instead.
    */
    IterMode[IterMode["IgnoreMounts"] = 4] = "IgnoreMounts";
    /**
    This option only applies in
    [`enter`](#common.SyntaxNode.enter)-style methods. It tells the
    library to not enter mounted overlays if one covers the given
    position.
    */
    IterMode[IterMode["IgnoreOverlays"] = 8] = "IgnoreOverlays";
    /**
    When set, positions on the boundary of a mounted overlay tree
    that has its [`bracketed`](#common.NestedParse.bracketed) flag
    set will enter that tree regardless of side. Only supported in
    [`enter`](#common.SyntaxNode.enter), not in cursors.
    */
    IterMode[IterMode["EnterBracketed"] = 16] = "EnterBracketed";
})(IterMode || (IterMode = {}));
/**
A piece of syntax tree. There are two ways to approach these
trees: the way they are actually stored in memory, and the
convenient way.

Syntax trees are stored as a tree of `Tree` and `TreeBuffer`
objects. By packing detail information into `TreeBuffer` leaf
nodes, the representation is made a lot more memory-efficient.

However, when you want to actually work with tree nodes, this
representation is very awkward, so most client code will want to
use the [`TreeCursor`](#common.TreeCursor) or
[`SyntaxNode`](#common.SyntaxNode) interface instead, which provides
a view on some part of this data structure, and can be used to
move around to adjacent nodes.
*/
class Tree {
    /**
    Construct a new tree. See also [`Tree.build`](#common.Tree^build).
    */
    constructor(
    /**
    The type of the top node.
    */
    type, 
    /**
    This node's child nodes.
    */
    children, 
    /**
    The positions (offsets relative to the start of this tree) of
    the children.
    */
    positions, 
    /**
    The total length of this tree
    */
    length, 
    /**
    Per-node [node props](#common.NodeProp) to associate with this node.
    */
    props) {
        this.type = type;
        this.children = children;
        this.positions = positions;
        this.length = length;
        /**
        @internal
        */
        this.props = null;
        if (props && props.length) {
            this.props = Object.create(null);
            for (let [prop, value] of props)
                this.props[typeof prop == "number" ? prop : prop.id] = value;
        }
    }
    /**
    @internal
    */
    toString() {
        let mounted = MountedTree.get(this);
        if (mounted && !mounted.overlay)
            return mounted.tree.toString();
        let children = "";
        for (let ch of this.children) {
            let str = ch.toString();
            if (str) {
                if (children)
                    children += ",";
                children += str;
            }
        }
        return !this.type.name ? children :
            (/\W/.test(this.type.name) && !this.type.isError ? JSON.stringify(this.type.name) : this.type.name) +
                (children.length ? "(" + children + ")" : "");
    }
    /**
    Get a [tree cursor](#common.TreeCursor) positioned at the top of
    the tree. Mode can be used to [control](#common.IterMode) which
    nodes the cursor visits.
    */
    cursor(mode = 0) {
        return new TreeCursor(this.topNode, mode);
    }
    /**
    Get a [tree cursor](#common.TreeCursor) pointing into this tree
    at the given position and side (see
    [`moveTo`](#common.TreeCursor.moveTo).
    */
    cursorAt(pos, side = 0, mode = 0) {
        let scope = CachedNode.get(this) || this.topNode;
        let cursor = new TreeCursor(scope);
        cursor.moveTo(pos, side);
        CachedNode.set(this, cursor._tree);
        return cursor;
    }
    /**
    Get a [syntax node](#common.SyntaxNode) object for the top of the
    tree.
    */
    get topNode() {
        return new TreeNode(this, 0, 0, null);
    }
    /**
    Get the [syntax node](#common.SyntaxNode) at the given position.
    If `side` is -1, this will move into nodes that end at the
    position. If 1, it'll move into nodes that start at the
    position. With 0, it'll only enter nodes that cover the position
    from both sides.
    
    Note that this will not enter
    [overlays](#common.MountedTree.overlay), and you often want
    [`resolveInner`](#common.Tree.resolveInner) instead.
    */
    resolve(pos, side = 0) {
        let node = resolveNode(CachedNode.get(this) || this.topNode, pos, side, false);
        CachedNode.set(this, node);
        return node;
    }
    /**
    Like [`resolve`](#common.Tree.resolve), but will enter
    [overlaid](#common.MountedTree.overlay) nodes, producing a syntax node
    pointing into the innermost overlaid tree at the given position
    (with parent links going through all parent structure, including
    the host trees).
    */
    resolveInner(pos, side = 0) {
        let node = resolveNode(CachedInnerNode.get(this) || this.topNode, pos, side, true);
        CachedInnerNode.set(this, node);
        return node;
    }
    /**
    In some situations, it can be useful to iterate through all
    nodes around a position, including those in overlays that don't
    directly cover the position. This method gives you an iterator
    that will produce all nodes, from small to big, around the given
    position.
    */
    resolveStack(pos, side = 0) {
        return stackIterator(this, pos, side);
    }
    /**
    Iterate over the tree and its children, calling `enter` for any
    node that touches the `from`/`to` region (if given) before
    running over such a node's children, and `leave` (if given) when
    leaving the node. When `enter` returns `false`, that node will
    not have its children iterated over (or `leave` called).
    */
    iterate(spec) {
        let { enter, leave, from = 0, to = this.length } = spec;
        let mode = spec.mode || 0, anon = (mode & IterMode.IncludeAnonymous) > 0;
        for (let c = this.cursor(mode | IterMode.IncludeAnonymous);;) {
            let entered = false;
            if (c.from <= to && c.to >= from && (!anon && c.type.isAnonymous || enter(c) !== false)) {
                if (c.firstChild())
                    continue;
                entered = true;
            }
            for (;;) {
                if (entered && leave && (anon || !c.type.isAnonymous))
                    leave(c);
                if (c.nextSibling())
                    break;
                if (!c.parent())
                    return;
                entered = true;
            }
        }
    }
    /**
    Get the value of the given [node prop](#common.NodeProp) for this
    node. Works with both per-node and per-type props.
    */
    prop(prop) {
        return !prop.perNode ? this.type.prop(prop) : this.props ? this.props[prop.id] : undefined;
    }
    /**
    Returns the node's [per-node props](#common.NodeProp.perNode) in a
    format that can be passed to the [`Tree`](#common.Tree)
    constructor.
    */
    get propValues() {
        let result = [];
        if (this.props)
            for (let id in this.props)
                result.push([+id, this.props[id]]);
        return result;
    }
    /**
    Balance the direct children of this tree, producing a copy of
    which may have children grouped into subtrees with type
    [`NodeType.none`](#common.NodeType^none).
    */
    balance(config = {}) {
        return this.children.length <= 8 /* Balance.BranchFactor */ ? this :
            balanceRange(NodeType.none, this.children, this.positions, 0, this.children.length, 0, this.length, (children, positions, length) => new Tree(this.type, children, positions, length, this.propValues), config.makeTree || ((children, positions, length) => new Tree(NodeType.none, children, positions, length)));
    }
    /**
    Build a tree from a postfix-ordered buffer of node information,
    or a cursor over such a buffer.
    */
    static build(data) { return buildTree(data); }
}
/**
The empty tree
*/
Tree.empty = new Tree(NodeType.none, [], [], 0);
class FlatBufferCursor {
    constructor(buffer, index) {
        this.buffer = buffer;
        this.index = index;
    }
    get id() { return this.buffer[this.index - 4]; }
    get start() { return this.buffer[this.index - 3]; }
    get end() { return this.buffer[this.index - 2]; }
    get size() { return this.buffer[this.index - 1]; }
    get pos() { return this.index; }
    next() { this.index -= 4; }
    fork() { return new FlatBufferCursor(this.buffer, this.index); }
}
/**
Tree buffers contain (type, start, end, endIndex) quads for each
node. In such a buffer, nodes are stored in prefix order (parents
before children, with the endIndex of the parent indicating which
children belong to it).
*/
class TreeBuffer {
    /**
    Create a tree buffer.
    */
    constructor(
    /**
    The buffer's content.
    */
    buffer, 
    /**
    The total length of the group of nodes in the buffer.
    */
    length, 
    /**
    The node set used in this buffer.
    */
    set) {
        this.buffer = buffer;
        this.length = length;
        this.set = set;
    }
    /**
    @internal
    */
    get type() { return NodeType.none; }
    /**
    @internal
    */
    toString() {
        let result = [];
        for (let index = 0; index < this.buffer.length;) {
            result.push(this.childString(index));
            index = this.buffer[index + 3];
        }
        return result.join(",");
    }
    /**
    @internal
    */
    childString(index) {
        let id = this.buffer[index], endIndex = this.buffer[index + 3];
        let type = this.set.types[id], result = type.name;
        if (/\W/.test(result) && !type.isError)
            result = JSON.stringify(result);
        index += 4;
        if (endIndex == index)
            return result;
        let children = [];
        while (index < endIndex) {
            children.push(this.childString(index));
            index = this.buffer[index + 3];
        }
        return result + "(" + children.join(",") + ")";
    }
    /**
    @internal
    */
    findChild(startIndex, endIndex, dir, pos, side) {
        let { buffer } = this, pick = -1;
        for (let i = startIndex; i != endIndex; i = buffer[i + 3]) {
            if (checkSide(side, pos, buffer[i + 1], buffer[i + 2])) {
                pick = i;
                if (dir > 0)
                    break;
            }
        }
        return pick;
    }
    /**
    @internal
    */
    slice(startI, endI, from) {
        let b = this.buffer;
        let copy = new Uint16Array(endI - startI), len = 0;
        for (let i = startI, j = 0; i < endI;) {
            copy[j++] = b[i++];
            copy[j++] = b[i++] - from;
            let to = copy[j++] = b[i++] - from;
            copy[j++] = b[i++] - startI;
            len = Math.max(len, to);
        }
        return new TreeBuffer(copy, len, this.set);
    }
}
function checkSide(side, pos, from, to) {
    switch (side) {
        case -2 /* Side.Before */: return from < pos;
        case -1 /* Side.AtOrBefore */: return to >= pos && from < pos;
        case 0 /* Side.Around */: return from < pos && to > pos;
        case 1 /* Side.AtOrAfter */: return from <= pos && to > pos;
        case 2 /* Side.After */: return to > pos;
        case 4 /* Side.DontCare */: return true;
    }
}
function resolveNode(node, pos, side, overlays) {
    var _a;
    // Move up to a node that actually holds the position, if possible
    while (node.from == node.to ||
        (side < 1 ? node.from >= pos : node.from > pos) ||
        (side > -1 ? node.to <= pos : node.to < pos)) {
        let parent = !overlays && node instanceof TreeNode && node.index < 0 ? null : node.parent;
        if (!parent)
            return node;
        node = parent;
    }
    let mode = overlays ? 0 : IterMode.IgnoreOverlays;
    // Must go up out of overlays when those do not overlap with pos
    if (overlays)
        for (let scan = node, parent = scan.parent; parent; scan = parent, parent = scan.parent) {
            if (scan instanceof TreeNode && scan.index < 0 && ((_a = parent.enter(pos, side, mode)) === null || _a === void 0 ? void 0 : _a.from) != scan.from)
                node = parent;
        }
    for (;;) {
        let inner = node.enter(pos, side, mode);
        if (!inner)
            return node;
        node = inner;
    }
}
class BaseNode {
    cursor(mode = 0) { return new TreeCursor(this, mode); }
    getChild(type, before = null, after = null) {
        let r = getChildren(this, type, before, after);
        return r.length ? r[0] : null;
    }
    getChildren(type, before = null, after = null) {
        return getChildren(this, type, before, after);
    }
    resolve(pos, side = 0) {
        return resolveNode(this, pos, side, false);
    }
    resolveInner(pos, side = 0) {
        return resolveNode(this, pos, side, true);
    }
    matchContext(context) {
        return matchNodeContext(this.parent, context);
    }
    enterUnfinishedNodesBefore(pos) {
        let scan = this.childBefore(pos), node = this;
        while (scan) {
            let last = scan.lastChild;
            if (!last || last.to != scan.to)
                break;
            if (last.type.isError && last.from == last.to) {
                node = scan;
                scan = last.prevSibling;
            }
            else {
                scan = last;
            }
        }
        return node;
    }
    get node() { return this; }
    get next() { return this.parent; }
}
class TreeNode extends BaseNode {
    constructor(_tree, from, 
    // Index in parent node, set to -1 if the node is not a direct child of _parent.node (overlay)
    index, _parent) {
        super();
        this._tree = _tree;
        this.from = from;
        this.index = index;
        this._parent = _parent;
    }
    get type() { return this._tree.type; }
    get name() { return this._tree.type.name; }
    get to() { return this.from + this._tree.length; }
    nextChild(i, dir, pos, side, mode = 0) {
        var _a;
        for (let parent = this;;) {
            for (let { children, positions } = parent._tree, e = dir > 0 ? children.length : -1; i != e; i += dir) {
                let next = children[i], start = positions[i] + parent.from;
                if (!((mode & IterMode.EnterBracketed) && next instanceof Tree &&
                    ((_a = MountedTree.get(next)) === null || _a === void 0 ? void 0 : _a.overlay) === null && (start >= pos || start + next.length <= pos)) &&
                    !checkSide(side, pos, start, start + next.length))
                    continue;
                if (next instanceof TreeBuffer) {
                    if (mode & IterMode.ExcludeBuffers)
                        continue;
                    let index = next.findChild(0, next.buffer.length, dir, pos - start, side);
                    if (index > -1)
                        return new BufferNode(new BufferContext(parent, next, i, start), null, index);
                }
                else if ((mode & IterMode.IncludeAnonymous) || (!next.type.isAnonymous || hasChild(next))) {
                    let mounted;
                    if (!(mode & IterMode.IgnoreMounts) && (mounted = MountedTree.get(next)) && !mounted.overlay)
                        return new TreeNode(mounted.tree, start, i, parent);
                    let inner = new TreeNode(next, start, i, parent);
                    return (mode & IterMode.IncludeAnonymous) || !inner.type.isAnonymous ? inner
                        : inner.nextChild(dir < 0 ? next.children.length - 1 : 0, dir, pos, side, mode);
                }
            }
            if ((mode & IterMode.IncludeAnonymous) || !parent.type.isAnonymous)
                return null;
            if (parent.index >= 0)
                i = parent.index + dir;
            else
                i = dir < 0 ? -1 : parent._parent._tree.children.length;
            parent = parent._parent;
            if (!parent)
                return null;
        }
    }
    get firstChild() { return this.nextChild(0, 1, 0, 4 /* Side.DontCare */); }
    get lastChild() { return this.nextChild(this._tree.children.length - 1, -1, 0, 4 /* Side.DontCare */); }
    childAfter(pos) { return this.nextChild(0, 1, pos, 2 /* Side.After */); }
    childBefore(pos) { return this.nextChild(this._tree.children.length - 1, -1, pos, -2 /* Side.Before */); }
    prop(prop) { return this._tree.prop(prop); }
    enter(pos, side, mode = 0) {
        let mounted;
        if (!(mode & IterMode.IgnoreOverlays) && (mounted = MountedTree.get(this._tree)) && mounted.overlay) {
            let rPos = pos - this.from, enterBracketed = (mode & IterMode.EnterBracketed) && mounted.bracketed;
            for (let { from, to } of mounted.overlay) {
                if ((side > 0 || enterBracketed ? from <= rPos : from < rPos) &&
                    (side < 0 || enterBracketed ? to >= rPos : to > rPos))
                    return new TreeNode(mounted.tree, mounted.overlay[0].from + this.from, -1, this);
            }
        }
        return this.nextChild(0, 1, pos, side, mode);
    }
    nextSignificantParent() {
        let val = this;
        while (val.type.isAnonymous && val._parent)
            val = val._parent;
        return val;
    }
    get parent() {
        return this._parent ? this._parent.nextSignificantParent() : null;
    }
    get nextSibling() {
        return this._parent && this.index >= 0 ? this._parent.nextChild(this.index + 1, 1, 0, 4 /* Side.DontCare */) : null;
    }
    get prevSibling() {
        return this._parent && this.index >= 0 ? this._parent.nextChild(this.index - 1, -1, 0, 4 /* Side.DontCare */) : null;
    }
    get tree() { return this._tree; }
    toTree() { return this._tree; }
    /**
    @internal
    */
    toString() { return this._tree.toString(); }
}
function getChildren(node, type, before, after) {
    let cur = node.cursor(), result = [];
    if (!cur.firstChild())
        return result;
    if (before != null)
        for (let found = false; !found;) {
            found = cur.type.is(before);
            if (!cur.nextSibling())
                return result;
        }
    for (;;) {
        if (after != null && cur.type.is(after))
            return result;
        if (cur.type.is(type))
            result.push(cur.node);
        if (!cur.nextSibling())
            return after == null ? result : [];
    }
}
function matchNodeContext(node, context, i = context.length - 1) {
    for (let p = node; i >= 0; p = p.parent) {
        if (!p)
            return false;
        if (!p.type.isAnonymous) {
            if (context[i] && context[i] != p.name)
                return false;
            i--;
        }
    }
    return true;
}
class BufferContext {
    constructor(parent, buffer, index, start) {
        this.parent = parent;
        this.buffer = buffer;
        this.index = index;
        this.start = start;
    }
}
class BufferNode extends BaseNode {
    get name() { return this.type.name; }
    get from() { return this.context.start + this.context.buffer.buffer[this.index + 1]; }
    get to() { return this.context.start + this.context.buffer.buffer[this.index + 2]; }
    constructor(context, _parent, index) {
        super();
        this.context = context;
        this._parent = _parent;
        this.index = index;
        this.type = context.buffer.set.types[context.buffer.buffer[index]];
    }
    child(dir, pos, side) {
        let { buffer } = this.context;
        let index = buffer.findChild(this.index + 4, buffer.buffer[this.index + 3], dir, pos - this.context.start, side);
        return index < 0 ? null : new BufferNode(this.context, this, index);
    }
    get firstChild() { return this.child(1, 0, 4 /* Side.DontCare */); }
    get lastChild() { return this.child(-1, 0, 4 /* Side.DontCare */); }
    childAfter(pos) { return this.child(1, pos, 2 /* Side.After */); }
    childBefore(pos) { return this.child(-1, pos, -2 /* Side.Before */); }
    prop(prop) { return this.type.prop(prop); }
    enter(pos, side, mode = 0) {
        if (mode & IterMode.ExcludeBuffers)
            return null;
        let { buffer } = this.context;
        let index = buffer.findChild(this.index + 4, buffer.buffer[this.index + 3], side > 0 ? 1 : -1, pos - this.context.start, side);
        return index < 0 ? null : new BufferNode(this.context, this, index);
    }
    get parent() {
        return this._parent || this.context.parent.nextSignificantParent();
    }
    externalSibling(dir) {
        return this._parent ? null : this.context.parent.nextChild(this.context.index + dir, dir, 0, 4 /* Side.DontCare */);
    }
    get nextSibling() {
        let { buffer } = this.context;
        let after = buffer.buffer[this.index + 3];
        if (after < (this._parent ? buffer.buffer[this._parent.index + 3] : buffer.buffer.length))
            return new BufferNode(this.context, this._parent, after);
        return this.externalSibling(1);
    }
    get prevSibling() {
        let { buffer } = this.context;
        let parentStart = this._parent ? this._parent.index + 4 : 0;
        if (this.index == parentStart)
            return this.externalSibling(-1);
        return new BufferNode(this.context, this._parent, buffer.findChild(parentStart, this.index, -1, 0, 4 /* Side.DontCare */));
    }
    get tree() { return null; }
    toTree() {
        let children = [], positions = [];
        let { buffer } = this.context;
        let startI = this.index + 4, endI = buffer.buffer[this.index + 3];
        if (endI > startI) {
            let from = buffer.buffer[this.index + 1];
            children.push(buffer.slice(startI, endI, from));
            positions.push(0);
        }
        return new Tree(this.type, children, positions, this.to - this.from);
    }
    /**
    @internal
    */
    toString() { return this.context.buffer.childString(this.index); }
}
function iterStack(heads) {
    if (!heads.length)
        return null;
    let pick = 0, picked = heads[0];
    for (let i = 1; i < heads.length; i++) {
        let node = heads[i];
        if (node.from > picked.from || node.to < picked.to) {
            picked = node;
            pick = i;
        }
    }
    let next = picked instanceof TreeNode && picked.index < 0 ? null : picked.parent;
    let newHeads = heads.slice();
    if (next)
        newHeads[pick] = next;
    else
        newHeads.splice(pick, 1);
    return new StackIterator(newHeads, picked);
}
class StackIterator {
    constructor(heads, node) {
        this.heads = heads;
        this.node = node;
    }
    get next() { return iterStack(this.heads); }
}
function stackIterator(tree, pos, side) {
    let inner = tree.resolveInner(pos, side), layers = null;
    for (let scan = inner instanceof TreeNode ? inner : inner.context.parent; scan; scan = scan.parent) {
        if (scan.index < 0) { // This is an overlay root
            let parent = scan.parent;
            (layers || (layers = [inner])).push(parent.resolve(pos, side));
            scan = parent;
        }
        else {
            let mount = MountedTree.get(scan.tree);
            // Relevant overlay branching off
            if (mount && mount.overlay && mount.overlay[0].from <= pos && mount.overlay[mount.overlay.length - 1].to >= pos) {
                let root = new TreeNode(mount.tree, mount.overlay[0].from + scan.from, -1, scan);
                (layers || (layers = [inner])).push(resolveNode(root, pos, side, false));
            }
        }
    }
    return layers ? iterStack(layers) : inner;
}
/**
A tree cursor object focuses on a given node in a syntax tree, and
allows you to move to adjacent nodes.
*/
class TreeCursor {
    /**
    Shorthand for `.type.name`.
    */
    get name() { return this.type.name; }
    /**
    @internal
    */
    constructor(node, mode = 0) {
        /**
        @internal
        */
        this.buffer = null;
        this.stack = [];
        /**
        @internal
        */
        this.index = 0;
        this.bufferNode = null;
        this.mode = mode & ~IterMode.EnterBracketed;
        if (node instanceof TreeNode) {
            this.yieldNode(node);
        }
        else {
            this._tree = node.context.parent;
            this.buffer = node.context;
            for (let n = node._parent; n; n = n._parent)
                this.stack.unshift(n.index);
            this.bufferNode = node;
            this.yieldBuf(node.index);
        }
    }
    yieldNode(node) {
        if (!node)
            return false;
        this._tree = node;
        this.type = node.type;
        this.from = node.from;
        this.to = node.to;
        return true;
    }
    yieldBuf(index, type) {
        this.index = index;
        let { start, buffer } = this.buffer;
        this.type = type || buffer.set.types[buffer.buffer[index]];
        this.from = start + buffer.buffer[index + 1];
        this.to = start + buffer.buffer[index + 2];
        return true;
    }
    /**
    @internal
    */
    yield(node) {
        if (!node)
            return false;
        if (node instanceof TreeNode) {
            this.buffer = null;
            return this.yieldNode(node);
        }
        this.buffer = node.context;
        return this.yieldBuf(node.index, node.type);
    }
    /**
    @internal
    */
    toString() {
        return this.buffer ? this.buffer.buffer.childString(this.index) : this._tree.toString();
    }
    /**
    @internal
    */
    enterChild(dir, pos, side) {
        if (!this.buffer)
            return this.yield(this._tree.nextChild(dir < 0 ? this._tree._tree.children.length - 1 : 0, dir, pos, side, this.mode));
        let { buffer } = this.buffer;
        let index = buffer.findChild(this.index + 4, buffer.buffer[this.index + 3], dir, pos - this.buffer.start, side);
        if (index < 0)
            return false;
        this.stack.push(this.index);
        return this.yieldBuf(index);
    }
    /**
    Move the cursor to this node's first child. When this returns
    false, the node has no child, and the cursor has not been moved.
    */
    firstChild() { return this.enterChild(1, 0, 4 /* Side.DontCare */); }
    /**
    Move the cursor to this node's last child.
    */
    lastChild() { return this.enterChild(-1, 0, 4 /* Side.DontCare */); }
    /**
    Move the cursor to the first child that ends after `pos`.
    */
    childAfter(pos) { return this.enterChild(1, pos, 2 /* Side.After */); }
    /**
    Move to the last child that starts before `pos`.
    */
    childBefore(pos) { return this.enterChild(-1, pos, -2 /* Side.Before */); }
    /**
    Move the cursor to the child around `pos`. If side is -1 the
    child may end at that position, when 1 it may start there. This
    will also enter [overlaid](#common.MountedTree.overlay)
    [mounted](#common.NodeProp^mounted) trees unless `overlays` is
    set to false.
    */
    enter(pos, side, mode = this.mode) {
        if (!this.buffer)
            return this.yield(this._tree.enter(pos, side, mode));
        return mode & IterMode.ExcludeBuffers ? false : this.enterChild(1, pos, side);
    }
    /**
    Move to the node's parent node, if this isn't the top node.
    */
    parent() {
        if (!this.buffer)
            return this.yieldNode((this.mode & IterMode.IncludeAnonymous) ? this._tree._parent : this._tree.parent);
        if (this.stack.length)
            return this.yieldBuf(this.stack.pop());
        let parent = (this.mode & IterMode.IncludeAnonymous) ? this.buffer.parent : this.buffer.parent.nextSignificantParent();
        this.buffer = null;
        return this.yieldNode(parent);
    }
    /**
    @internal
    */
    sibling(dir) {
        if (!this.buffer)
            return !this._tree._parent ? false
                : this.yield(this._tree.index < 0 ? null
                    : this._tree._parent.nextChild(this._tree.index + dir, dir, 0, 4 /* Side.DontCare */, this.mode));
        let { buffer } = this.buffer, d = this.stack.length - 1;
        if (dir < 0) {
            let parentStart = d < 0 ? 0 : this.stack[d] + 4;
            if (this.index != parentStart)
                return this.yieldBuf(buffer.findChild(parentStart, this.index, -1, 0, 4 /* Side.DontCare */));
        }
        else {
            let after = buffer.buffer[this.index + 3];
            if (after < (d < 0 ? buffer.buffer.length : buffer.buffer[this.stack[d] + 3]))
                return this.yieldBuf(after);
        }
        return d < 0 ? this.yield(this.buffer.parent.nextChild(this.buffer.index + dir, dir, 0, 4 /* Side.DontCare */, this.mode)) : false;
    }
    /**
    Move to this node's next sibling, if any.
    */
    nextSibling() { return this.sibling(1); }
    /**
    Move to this node's previous sibling, if any.
    */
    prevSibling() { return this.sibling(-1); }
    atLastNode(dir) {
        let index, parent, { buffer } = this;
        if (buffer) {
            if (dir > 0) {
                if (this.index < buffer.buffer.buffer.length)
                    return false;
            }
            else {
                for (let i = 0; i < this.index; i++)
                    if (buffer.buffer.buffer[i + 3] < this.index)
                        return false;
            }
            ({ index, parent } = buffer);
        }
        else {
            ({ index, _parent: parent } = this._tree);
        }
        for (; parent; { index, _parent: parent } = parent) {
            if (index > -1)
                for (let i = index + dir, e = dir < 0 ? -1 : parent._tree.children.length; i != e; i += dir) {
                    let child = parent._tree.children[i];
                    if ((this.mode & IterMode.IncludeAnonymous) ||
                        child instanceof TreeBuffer ||
                        !child.type.isAnonymous ||
                        hasChild(child))
                        return false;
                }
        }
        return true;
    }
    move(dir, enter) {
        if (enter && this.enterChild(dir, 0, 4 /* Side.DontCare */))
            return true;
        for (;;) {
            if (this.sibling(dir))
                return true;
            if (this.atLastNode(dir) || !this.parent())
                return false;
        }
    }
    /**
    Move to the next node in a
    [pre-order](https://en.wikipedia.org/wiki/Tree_traversal#Pre-order,_NLR)
    traversal, going from a node to its first child or, if the
    current node is empty or `enter` is false, its next sibling or
    the next sibling of the first parent node that has one.
    */
    next(enter = true) { return this.move(1, enter); }
    /**
    Move to the next node in a last-to-first pre-order traversal. A
    node is followed by its last child or, if it has none, its
    previous sibling or the previous sibling of the first parent
    node that has one.
    */
    prev(enter = true) { return this.move(-1, enter); }
    /**
    Move the cursor to the innermost node that covers `pos`. If
    `side` is -1, it will enter nodes that end at `pos`. If it is 1,
    it will enter nodes that start at `pos`.
    */
    moveTo(pos, side = 0) {
        // Move up to a node that actually holds the position, if possible
        while (this.from == this.to ||
            (side < 1 ? this.from >= pos : this.from > pos) ||
            (side > -1 ? this.to <= pos : this.to < pos))
            if (!this.parent())
                break;
        // Then scan down into child nodes as far as possible
        while (this.enterChild(1, pos, side)) { }
        return this;
    }
    /**
    Get a [syntax node](#common.SyntaxNode) at the cursor's current
    position.
    */
    get node() {
        if (!this.buffer)
            return this._tree;
        let cache = this.bufferNode, result = null, depth = 0;
        if (cache && cache.context == this.buffer) {
            scan: for (let index = this.index, d = this.stack.length; d >= 0;) {
                for (let c = cache; c; c = c._parent)
                    if (c.index == index) {
                        if (index == this.index)
                            return c;
                        result = c;
                        depth = d + 1;
                        break scan;
                    }
                index = this.stack[--d];
            }
        }
        for (let i = depth; i < this.stack.length; i++)
            result = new BufferNode(this.buffer, result, this.stack[i]);
        return this.bufferNode = new BufferNode(this.buffer, result, this.index);
    }
    /**
    Get the [tree](#common.Tree) that represents the current node, if
    any. Will return null when the node is in a [tree
    buffer](#common.TreeBuffer).
    */
    get tree() {
        return this.buffer ? null : this._tree._tree;
    }
    /**
    Iterate over the current node and all its descendants, calling
    `enter` when entering a node and `leave`, if given, when leaving
    one. When `enter` returns `false`, any children of that node are
    skipped, and `leave` isn't called for it.
    */
    iterate(enter, leave) {
        for (let depth = 0;;) {
            let mustLeave = false;
            if (this.type.isAnonymous || enter(this) !== false) {
                if (this.firstChild()) {
                    depth++;
                    continue;
                }
                if (!this.type.isAnonymous)
                    mustLeave = true;
            }
            for (;;) {
                if (mustLeave && leave)
                    leave(this);
                mustLeave = this.type.isAnonymous;
                if (!depth)
                    return;
                if (this.nextSibling())
                    break;
                this.parent();
                depth--;
                mustLeave = true;
            }
        }
    }
    /**
    Test whether the current node matches a given context—a sequence
    of direct parent node names. Empty strings in the context array
    are treated as wildcards.
    */
    matchContext(context) {
        if (!this.buffer)
            return matchNodeContext(this.node.parent, context);
        let { buffer } = this.buffer, { types } = buffer.set;
        for (let i = context.length - 1, d = this.stack.length - 1; i >= 0; d--) {
            if (d < 0)
                return matchNodeContext(this._tree, context, i);
            let type = types[buffer.buffer[this.stack[d]]];
            if (!type.isAnonymous) {
                if (context[i] && context[i] != type.name)
                    return false;
                i--;
            }
        }
        return true;
    }
}
function hasChild(tree) {
    return tree.children.some(ch => ch instanceof TreeBuffer || !ch.type.isAnonymous || hasChild(ch));
}
function buildTree(data) {
    var _a;
    let { buffer, nodeSet, maxBufferLength = DefaultBufferLength, reused = [], minRepeatType = nodeSet.types.length } = data;
    let cursor = Array.isArray(buffer) ? new FlatBufferCursor(buffer, buffer.length) : buffer;
    let types = nodeSet.types;
    let contextHash = 0, lookAhead = 0;
    function takeNode(parentStart, minPos, children, positions, inRepeat, depth) {
        let { id, start, end, size } = cursor;
        let lookAheadAtStart = lookAhead, contextAtStart = contextHash;
        if (size < 0) {
            cursor.next();
            if (size == -1 /* SpecialRecord.Reuse */) {
                let node = reused[id];
                children.push(node);
                positions.push(start - parentStart);
                return;
            }
            else if (size == -3 /* SpecialRecord.ContextChange */) { // Context change
                contextHash = id;
                return;
            }
            else if (size == -4 /* SpecialRecord.LookAhead */) {
                lookAhead = id;
                return;
            }
            else {
                throw new RangeError(`Unrecognized record size: ${size}`);
            }
        }
        let type = types[id], node, buffer;
        let startPos = start - parentStart;
        if (end - start <= maxBufferLength && (buffer = findBufferSize(cursor.pos - minPos, inRepeat))) {
            // Small enough for a buffer, and no reused nodes inside
            let data = new Uint16Array(buffer.size - buffer.skip);
            let endPos = cursor.pos - buffer.size, index = data.length;
            while (cursor.pos > endPos)
                index = copyToBuffer(buffer.start, data, index);
            node = new TreeBuffer(data, end - buffer.start, nodeSet);
            startPos = buffer.start - parentStart;
        }
        else { // Make it a node
            let endPos = cursor.pos - size;
            cursor.next();
            let localChildren = [], localPositions = [];
            let localInRepeat = id >= minRepeatType ? id : -1;
            let lastGroup = 0, lastEnd = end;
            while (cursor.pos > endPos) {
                if (localInRepeat >= 0 && cursor.id == localInRepeat && cursor.size >= 0) {
                    if (cursor.end <= lastEnd - maxBufferLength) {
                        makeRepeatLeaf(localChildren, localPositions, start, lastGroup, cursor.end, lastEnd, localInRepeat, lookAheadAtStart, contextAtStart);
                        lastGroup = localChildren.length;
                        lastEnd = cursor.end;
                    }
                    cursor.next();
                }
                else if (depth > 2500 /* CutOff.Depth */) {
                    takeFlatNode(start, endPos, localChildren, localPositions);
                }
                else {
                    takeNode(start, endPos, localChildren, localPositions, localInRepeat, depth + 1);
                }
            }
            if (localInRepeat >= 0 && lastGroup > 0 && lastGroup < localChildren.length)
                makeRepeatLeaf(localChildren, localPositions, start, lastGroup, start, lastEnd, localInRepeat, lookAheadAtStart, contextAtStart);
            localChildren.reverse();
            localPositions.reverse();
            if (localInRepeat > -1 && lastGroup > 0) {
                let make = makeBalanced(type, contextAtStart);
                node = balanceRange(type, localChildren, localPositions, 0, localChildren.length, 0, end - start, make, make);
            }
            else {
                node = makeTree(type, localChildren, localPositions, end - start, lookAheadAtStart - end, contextAtStart);
            }
        }
        children.push(node);
        positions.push(startPos);
    }
    function takeFlatNode(parentStart, minPos, children, positions) {
        let nodes = []; // Temporary, inverted array of leaf nodes found, with absolute positions
        let nodeCount = 0, stopAt = -1;
        while (cursor.pos > minPos) {
            let { id, start, end, size } = cursor;
            if (size > 4) { // Not a leaf
                cursor.next();
            }
            else if (stopAt > -1 && start < stopAt) {
                break;
            }
            else {
                if (stopAt < 0)
                    stopAt = end - maxBufferLength;
                nodes.push(id, start, end);
                nodeCount++;
                cursor.next();
            }
        }
        if (nodeCount) {
            let buffer = new Uint16Array(nodeCount * 4);
            let start = nodes[nodes.length - 2];
            for (let i = nodes.length - 3, j = 0; i >= 0; i -= 3) {
                buffer[j++] = nodes[i];
                buffer[j++] = nodes[i + 1] - start;
                buffer[j++] = nodes[i + 2] - start;
                buffer[j++] = j;
            }
            children.push(new TreeBuffer(buffer, nodes[2] - start, nodeSet));
            positions.push(start - parentStart);
        }
    }
    function makeBalanced(type, contextHash) {
        return (children, positions, length) => {
            let lookAhead = 0, lastI = children.length - 1, last, lookAheadProp;
            if (lastI >= 0 && (last = children[lastI]) instanceof Tree) {
                if (!lastI && last.type == type && last.length == length)
                    return last;
                if (lookAheadProp = last.prop(NodeProp.lookAhead))
                    lookAhead = positions[lastI] + last.length + lookAheadProp;
            }
            return makeTree(type, children, positions, length, lookAhead, contextHash);
        };
    }
    function makeRepeatLeaf(children, positions, base, i, from, to, type, lookAhead, contextHash) {
        let localChildren = [], localPositions = [];
        while (children.length > i) {
            localChildren.push(children.pop());
            localPositions.push(positions.pop() + base - from);
        }
        children.push(makeTree(nodeSet.types[type], localChildren, localPositions, to - from, lookAhead - to, contextHash));
        positions.push(from - base);
    }
    function makeTree(type, children, positions, length, lookAhead, contextHash, props) {
        if (contextHash) {
            let pair = [NodeProp.contextHash, contextHash];
            props = props ? [pair].concat(props) : [pair];
        }
        if (lookAhead > 25) {
            let pair = [NodeProp.lookAhead, lookAhead];
            props = props ? [pair].concat(props) : [pair];
        }
        return new Tree(type, children, positions, length, props);
    }
    function findBufferSize(maxSize, inRepeat) {
        // Scan through the buffer to find previous siblings that fit
        // together in a TreeBuffer, and don't contain any reused nodes
        // (which can't be stored in a buffer).
        // If `inRepeat` is > -1, ignore node boundaries of that type for
        // nesting, but make sure the end falls either at the start
        // (`maxSize`) or before such a node.
        let fork = cursor.fork();
        let size = 0, start = 0, skip = 0, minStart = fork.end - maxBufferLength;
        let result = { size: 0, start: 0, skip: 0 };
        scan: for (let minPos = fork.pos - maxSize; fork.pos > minPos;) {
            let nodeSize = fork.size;
            // Pretend nested repeat nodes of the same type don't exist
            if (fork.id == inRepeat && nodeSize >= 0) {
                // Except that we store the current state as a valid return
                // value.
                result.size = size;
                result.start = start;
                result.skip = skip;
                skip += 4;
                size += 4;
                fork.next();
                continue;
            }
            let startPos = fork.pos - nodeSize;
            if (nodeSize < 0 || startPos < minPos || fork.start < minStart)
                break;
            let localSkipped = fork.id >= minRepeatType ? 4 : 0;
            let nodeStart = fork.start;
            fork.next();
            while (fork.pos > startPos) {
                if (fork.size < 0) {
                    if (fork.size == -3 /* SpecialRecord.ContextChange */ || fork.size == -4 /* SpecialRecord.LookAhead */)
                        localSkipped += 4;
                    else
                        break scan;
                }
                else if (fork.id >= minRepeatType) {
                    localSkipped += 4;
                }
                fork.next();
            }
            start = nodeStart;
            size += nodeSize;
            skip += localSkipped;
        }
        if (inRepeat < 0 || size == maxSize) {
            result.size = size;
            result.start = start;
            result.skip = skip;
        }
        return result.size > 4 ? result : undefined;
    }
    function copyToBuffer(bufferStart, buffer, index) {
        let { id, start, end, size } = cursor;
        cursor.next();
        if (size >= 0 && id < minRepeatType) {
            let startIndex = index;
            if (size > 4) {
                let endPos = cursor.pos - (size - 4);
                while (cursor.pos > endPos)
                    index = copyToBuffer(bufferStart, buffer, index);
            }
            buffer[--index] = startIndex;
            buffer[--index] = end - bufferStart;
            buffer[--index] = start - bufferStart;
            buffer[--index] = id;
        }
        else if (size == -3 /* SpecialRecord.ContextChange */) {
            contextHash = id;
        }
        else if (size == -4 /* SpecialRecord.LookAhead */) {
            lookAhead = id;
        }
        return index;
    }
    let children = [], positions = [];
    while (cursor.pos > 0)
        takeNode(data.start || 0, data.bufferStart || 0, children, positions, -1, 0);
    let length = (_a = data.length) !== null && _a !== void 0 ? _a : (children.length ? positions[0] + children[0].length : 0);
    return new Tree(types[data.topID], children.reverse(), positions.reverse(), length);
}
const nodeSizeCache = new WeakMap;
function nodeSize(balanceType, node) {
    if (!balanceType.isAnonymous || node instanceof TreeBuffer || node.type != balanceType)
        return 1;
    let size = nodeSizeCache.get(node);
    if (size == null) {
        size = 1;
        for (let child of node.children) {
            if (child.type != balanceType || !(child instanceof Tree)) {
                size = 1;
                break;
            }
            size += nodeSize(balanceType, child);
        }
        nodeSizeCache.set(node, size);
    }
    return size;
}
function balanceRange(
// The type the balanced tree's inner nodes.
balanceType, 
// The direct children and their positions
children, positions, 
// The index range in children/positions to use
from, to, 
// The start position of the nodes, relative to their parent.
start, 
// Length of the outer node
length, 
// Function to build the top node of the balanced tree
mkTop, 
// Function to build internal nodes for the balanced tree
mkTree) {
    let total = 0;
    for (let i = from; i < to; i++)
        total += nodeSize(balanceType, children[i]);
    let maxChild = Math.ceil((total * 1.5) / 8 /* Balance.BranchFactor */);
    let localChildren = [], localPositions = [];
    function divide(children, positions, from, to, offset) {
        for (let i = from; i < to;) {
            let groupFrom = i, groupStart = positions[i], groupSize = nodeSize(balanceType, children[i]);
            i++;
            for (; i < to; i++) {
                let nextSize = nodeSize(balanceType, children[i]);
                if (groupSize + nextSize >= maxChild)
                    break;
                groupSize += nextSize;
            }
            if (i == groupFrom + 1) {
                if (groupSize > maxChild) {
                    let only = children[groupFrom]; // Only trees can have a size > 1
                    divide(only.children, only.positions, 0, only.children.length, positions[groupFrom] + offset);
                    continue;
                }
                localChildren.push(children[groupFrom]);
            }
            else {
                let length = positions[i - 1] + children[i - 1].length - groupStart;
                localChildren.push(balanceRange(balanceType, children, positions, groupFrom, i, groupStart, length, null, mkTree));
            }
            localPositions.push(groupStart + offset - start);
        }
    }
    divide(children, positions, from, to, 0);
    return (mkTop || mkTree)(localChildren, localPositions, length);
}
/**
Provides a way to associate values with pieces of trees. As long
as that part of the tree is reused, the associated values can be
retrieved from an updated tree.
*/
class NodeWeakMap {
    constructor() {
        this.map = new WeakMap();
    }
    setBuffer(buffer, index, value) {
        let inner = this.map.get(buffer);
        if (!inner)
            this.map.set(buffer, inner = new Map);
        inner.set(index, value);
    }
    getBuffer(buffer, index) {
        let inner = this.map.get(buffer);
        return inner && inner.get(index);
    }
    /**
    Set the value for this syntax node.
    */
    set(node, value) {
        if (node instanceof BufferNode)
            this.setBuffer(node.context.buffer, node.index, value);
        else if (node instanceof TreeNode)
            this.map.set(node.tree, value);
    }
    /**
    Retrieve value for this syntax node, if it exists in the map.
    */
    get(node) {
        return node instanceof BufferNode ? this.getBuffer(node.context.buffer, node.index)
            : node instanceof TreeNode ? this.map.get(node.tree) : undefined;
    }
    /**
    Set the value for the node that a cursor currently points to.
    */
    cursorSet(cursor, value) {
        if (cursor.buffer)
            this.setBuffer(cursor.buffer.buffer, cursor.index, value);
        else
            this.map.set(cursor.tree, value);
    }
    /**
    Retrieve the value for the node that a cursor currently points
    to.
    */
    cursorGet(cursor) {
        return cursor.buffer ? this.getBuffer(cursor.buffer.buffer, cursor.index) : this.map.get(cursor.tree);
    }
}

/**
Tree fragments are used during [incremental
parsing](#common.Parser.startParse) to track parts of old trees
that can be reused in a new parse. An array of fragments is used
to track regions of an old tree whose nodes might be reused in new
parses. Use the static
[`applyChanges`](#common.TreeFragment^applyChanges) method to
update fragments for document changes.
*/
class TreeFragment {
    /**
    Construct a tree fragment. You'll usually want to use
    [`addTree`](#common.TreeFragment^addTree) and
    [`applyChanges`](#common.TreeFragment^applyChanges) instead of
    calling this directly.
    */
    constructor(
    /**
    The start of the unchanged range pointed to by this fragment.
    This refers to an offset in the _updated_ document (as opposed
    to the original tree).
    */
    from, 
    /**
    The end of the unchanged range.
    */
    to, 
    /**
    The tree that this fragment is based on.
    */
    tree, 
    /**
    The offset between the fragment's tree and the document that
    this fragment can be used against. Add this when going from
    document to tree positions, subtract it to go from tree to
    document positions.
    */
    offset, openStart = false, openEnd = false) {
        this.from = from;
        this.to = to;
        this.tree = tree;
        this.offset = offset;
        this.open = (openStart ? 1 /* Open.Start */ : 0) | (openEnd ? 2 /* Open.End */ : 0);
    }
    /**
    Whether the start of the fragment represents the start of a
    parse, or the end of a change. (In the second case, it may not
    be safe to reuse some nodes at the start, depending on the
    parsing algorithm.)
    */
    get openStart() { return (this.open & 1 /* Open.Start */) > 0; }
    /**
    Whether the end of the fragment represents the end of a
    full-document parse, or the start of a change.
    */
    get openEnd() { return (this.open & 2 /* Open.End */) > 0; }
    /**
    Create a set of fragments from a freshly parsed tree, or update
    an existing set of fragments by replacing the ones that overlap
    with a tree with content from the new tree. When `partial` is
    true, the parse is treated as incomplete, and the resulting
    fragment has [`openEnd`](#common.TreeFragment.openEnd) set to
    true.
    */
    static addTree(tree, fragments = [], partial = false) {
        let result = [new TreeFragment(0, tree.length, tree, 0, false, partial)];
        for (let f of fragments)
            if (f.to > tree.length)
                result.push(f);
        return result;
    }
    /**
    Apply a set of edits to an array of fragments, removing or
    splitting fragments as necessary to remove edited ranges, and
    adjusting offsets for fragments that moved.
    */
    static applyChanges(fragments, changes, minGap = 128) {
        if (!changes.length)
            return fragments;
        let result = [];
        let fI = 1, nextF = fragments.length ? fragments[0] : null;
        for (let cI = 0, pos = 0, off = 0;; cI++) {
            let nextC = cI < changes.length ? changes[cI] : null;
            let nextPos = nextC ? nextC.fromA : 1e9;
            if (nextPos - pos >= minGap)
                while (nextF && nextF.from < nextPos) {
                    let cut = nextF;
                    if (pos >= cut.from || nextPos <= cut.to || off) {
                        let fFrom = Math.max(cut.from, pos) - off, fTo = Math.min(cut.to, nextPos) - off;
                        cut = fFrom >= fTo ? null : new TreeFragment(fFrom, fTo, cut.tree, cut.offset + off, cI > 0, !!nextC);
                    }
                    if (cut)
                        result.push(cut);
                    if (nextF.to > nextPos)
                        break;
                    nextF = fI < fragments.length ? fragments[fI++] : null;
                }
            if (!nextC)
                break;
            pos = nextC.toA;
            off = nextC.toA - nextC.toB;
        }
        return result;
    }
}
/**
A superclass that parsers should extend.
*/
class Parser {
    /**
    Start a parse, returning a [partial parse](#common.PartialParse)
    object. [`fragments`](#common.TreeFragment) can be passed in to
    make the parse incremental.
    
    By default, the entire input is parsed. You can pass `ranges`,
    which should be a sorted array of non-empty, non-overlapping
    ranges, to parse only those ranges. The tree returned in that
    case will start at `ranges[0].from`.
    */
    startParse(input, fragments, ranges) {
        if (typeof input == "string")
            input = new StringInput(input);
        ranges = !ranges ? [new Range(0, input.length)] : ranges.length ? ranges.map(r => new Range(r.from, r.to)) : [new Range(0, 0)];
        return this.createParse(input, fragments || [], ranges);
    }
    /**
    Run a full parse, returning the resulting tree.
    */
    parse(input, fragments, ranges) {
        let parse = this.startParse(input, fragments, ranges);
        for (;;) {
            let done = parse.advance();
            if (done)
                return done;
        }
    }
}
class StringInput {
    constructor(string) {
        this.string = string;
    }
    get length() { return this.string.length; }
    chunk(from) { return this.string.slice(from); }
    get lineChunks() { return false; }
    read(from, to) { return this.string.slice(from, to); }
}

/**
Create a parse wrapper that, after the inner parse completes,
scans its tree for mixed language regions with the `nest`
function, runs the resulting [inner parses](#common.NestedParse),
and then [mounts](#common.NodeProp^mounted) their results onto the
tree.
*/
function parseMixed(nest) {
    return (parse, input, fragments, ranges) => new MixedParse(parse, nest, input, fragments, ranges);
}
class InnerParse {
    constructor(parser, parse, overlay, bracketed, target, from) {
        this.parser = parser;
        this.parse = parse;
        this.overlay = overlay;
        this.bracketed = bracketed;
        this.target = target;
        this.from = from;
    }
}
function checkRanges(ranges) {
    if (!ranges.length || ranges.some(r => r.from >= r.to))
        throw new RangeError("Invalid inner parse ranges given: " + JSON.stringify(ranges));
}
class ActiveOverlay {
    constructor(parser, predicate, mounts, index, start, bracketed, target, prev) {
        this.parser = parser;
        this.predicate = predicate;
        this.mounts = mounts;
        this.index = index;
        this.start = start;
        this.bracketed = bracketed;
        this.target = target;
        this.prev = prev;
        this.depth = 0;
        this.ranges = [];
    }
}
const stoppedInner = new NodeProp({ perNode: true });
class MixedParse {
    constructor(base, nest, input, fragments, ranges) {
        this.nest = nest;
        this.input = input;
        this.fragments = fragments;
        this.ranges = ranges;
        this.inner = [];
        this.innerDone = 0;
        this.baseTree = null;
        this.stoppedAt = null;
        this.baseParse = base;
    }
    advance() {
        if (this.baseParse) {
            let done = this.baseParse.advance();
            if (!done)
                return null;
            this.baseParse = null;
            this.baseTree = done;
            this.startInner();
            if (this.stoppedAt != null)
                for (let inner of this.inner)
                    inner.parse.stopAt(this.stoppedAt);
        }
        if (this.innerDone == this.inner.length) {
            let result = this.baseTree;
            if (this.stoppedAt != null)
                result = new Tree(result.type, result.children, result.positions, result.length, result.propValues.concat([[stoppedInner, this.stoppedAt]]));
            return result;
        }
        let inner = this.inner[this.innerDone], done = inner.parse.advance();
        if (done) {
            this.innerDone++;
            // This is a somewhat dodgy but super helpful hack where we
            // patch up nodes created by the inner parse (and thus
            // presumably not aliased anywhere else) to hold the information
            // about the inner parse.
            let props = Object.assign(Object.create(null), inner.target.props);
            props[NodeProp.mounted.id] = new MountedTree(done, inner.overlay, inner.parser, inner.bracketed);
            inner.target.props = props;
        }
        return null;
    }
    get parsedPos() {
        if (this.baseParse)
            return 0;
        let pos = this.input.length;
        for (let i = this.innerDone; i < this.inner.length; i++) {
            if (this.inner[i].from < pos)
                pos = Math.min(pos, this.inner[i].parse.parsedPos);
        }
        return pos;
    }
    stopAt(pos) {
        this.stoppedAt = pos;
        if (this.baseParse)
            this.baseParse.stopAt(pos);
        else
            for (let i = this.innerDone; i < this.inner.length; i++)
                this.inner[i].parse.stopAt(pos);
    }
    startInner() {
        let fragmentCursor = new FragmentCursor(this.fragments);
        let overlay = null;
        let covered = null;
        let cursor = new TreeCursor(new TreeNode(this.baseTree, this.ranges[0].from, 0, null), IterMode.IncludeAnonymous | IterMode.IgnoreMounts);
        scan: for (let nest, isCovered;;) {
            let enter = true, range;
            if (this.stoppedAt != null && cursor.from >= this.stoppedAt) {
                enter = false;
            }
            else if (fragmentCursor.hasNode(cursor)) {
                if (overlay) {
                    let match = overlay.mounts.find(m => m.frag.from <= cursor.from && m.frag.to >= cursor.to && m.mount.overlay);
                    if (match)
                        for (let r of match.mount.overlay) {
                            let from = r.from + match.pos, to = r.to + match.pos;
                            if (from >= cursor.from && to <= cursor.to && !overlay.ranges.some(r => r.from < to && r.to > from))
                                overlay.ranges.push({ from, to });
                        }
                }
                enter = false;
            }
            else if (covered && (isCovered = checkCover(covered.ranges, cursor.from, cursor.to))) {
                enter = isCovered != 2 /* Cover.Full */;
            }
            else if (!cursor.type.isAnonymous && (nest = this.nest(cursor, this.input)) &&
                (cursor.from < cursor.to || !nest.overlay)) {
                if (!cursor.tree) {
                    materialize(cursor);
                    // materialize create one more level of nesting
                    // we need to add depth to active overlay for going backwards
                    if (overlay)
                        overlay.depth++;
                    if (covered)
                        covered.depth++;
                }
                let oldMounts = fragmentCursor.findMounts(cursor.from, nest.parser);
                if (typeof nest.overlay == "function") {
                    overlay = new ActiveOverlay(nest.parser, nest.overlay, oldMounts, this.inner.length, cursor.from, !!nest.bracketed, cursor.tree, overlay);
                }
                else {
                    let ranges = punchRanges(this.ranges, nest.overlay ||
                        (cursor.from < cursor.to ? [new Range(cursor.from, cursor.to)] : []));
                    if (ranges.length)
                        checkRanges(ranges);
                    if (ranges.length || !nest.overlay)
                        this.inner.push(new InnerParse(nest.parser, ranges.length ? nest.parser.startParse(this.input, enterFragments(oldMounts, ranges), ranges)
                            : nest.parser.startParse(""), nest.overlay ? nest.overlay.map(r => new Range(r.from - cursor.from, r.to - cursor.from)) : null, !!nest.bracketed, cursor.tree, ranges.length ? ranges[0].from : cursor.from));
                    if (!nest.overlay)
                        enter = false;
                    else if (ranges.length)
                        covered = { ranges, depth: 0, prev: covered };
                }
            }
            else if (overlay && (range = overlay.predicate(cursor))) {
                if (range === true)
                    range = new Range(cursor.from, cursor.to);
                if (range.from < range.to) {
                    let last = overlay.ranges.length - 1;
                    if (last >= 0 && overlay.ranges[last].to == range.from)
                        overlay.ranges[last] = { from: overlay.ranges[last].from, to: range.to };
                    else
                        overlay.ranges.push(range);
                }
            }
            if (enter && cursor.firstChild()) {
                if (overlay)
                    overlay.depth++;
                if (covered)
                    covered.depth++;
            }
            else {
                for (;;) {
                    if (cursor.nextSibling())
                        break;
                    if (!cursor.parent())
                        break scan;
                    if (overlay && !--overlay.depth) {
                        let ranges = punchRanges(this.ranges, overlay.ranges);
                        if (ranges.length) {
                            checkRanges(ranges);
                            this.inner.splice(overlay.index, 0, new InnerParse(overlay.parser, overlay.parser.startParse(this.input, enterFragments(overlay.mounts, ranges), ranges), overlay.ranges.map(r => new Range(r.from - overlay.start, r.to - overlay.start)), overlay.bracketed, overlay.target, ranges[0].from));
                        }
                        overlay = overlay.prev;
                    }
                    if (covered && !--covered.depth)
                        covered = covered.prev;
                }
            }
        }
    }
}
function checkCover(covered, from, to) {
    for (let range of covered) {
        if (range.from >= to)
            break;
        if (range.to > from)
            return range.from <= from && range.to >= to ? 2 /* Cover.Full */ : 1 /* Cover.Partial */;
    }
    return 0 /* Cover.None */;
}
// Take a piece of buffer and convert it into a stand-alone
// TreeBuffer.
function sliceBuf(buf, startI, endI, nodes, positions, off) {
    if (startI < endI) {
        let from = buf.buffer[startI + 1];
        nodes.push(buf.slice(startI, endI, from));
        positions.push(from - off);
    }
}
// This function takes a node that's in a buffer, and converts it, and
// its parent buffer nodes, into a Tree. This is again acting on the
// assumption that the trees and buffers have been constructed by the
// parse that was ran via the mix parser, and thus aren't shared with
// any other code, making violations of the immutability safe.
function materialize(cursor) {
    let { node } = cursor, stack = [];
    let buffer = node.context.buffer;
    // Scan up to the nearest tree
    do {
        stack.push(cursor.index);
        cursor.parent();
    } while (!cursor.tree);
    // Find the index of the buffer in that tree
    let base = cursor.tree, i = base.children.indexOf(buffer);
    let buf = base.children[i], b = buf.buffer, newStack = [i];
    // Split a level in the buffer, putting the nodes before and after
    // the child that contains `node` into new buffers.
    function split(startI, endI, type, innerOffset, length, stackPos) {
        let targetI = stack[stackPos];
        let children = [], positions = [];
        sliceBuf(buf, startI, targetI, children, positions, innerOffset);
        let from = b[targetI + 1], to = b[targetI + 2];
        newStack.push(children.length);
        let child = stackPos
            ? split(targetI + 4, b[targetI + 3], buf.set.types[b[targetI]], from, to - from, stackPos - 1)
            : node.toTree();
        children.push(child);
        positions.push(from - innerOffset);
        sliceBuf(buf, b[targetI + 3], endI, children, positions, innerOffset);
        return new Tree(type, children, positions, length);
    }
    base.children[i] = split(0, b.length, NodeType.none, 0, buf.length, stack.length - 1);
    // Move the cursor back to the target node
    for (let index of newStack) {
        let tree = cursor.tree.children[index], pos = cursor.tree.positions[index];
        cursor.yield(new TreeNode(tree, pos + cursor.from, index, cursor._tree));
    }
}
class StructureCursor {
    constructor(root, offset) {
        this.offset = offset;
        this.done = false;
        this.cursor = root.cursor(IterMode.IncludeAnonymous | IterMode.IgnoreMounts);
    }
    // Move to the first node (in pre-order) that starts at or after `pos`.
    moveTo(pos) {
        let { cursor } = this, p = pos - this.offset;
        while (!this.done && cursor.from < p) {
            if (cursor.to >= pos && cursor.enter(p, 1, IterMode.IgnoreOverlays | IterMode.ExcludeBuffers)) ;
            else if (!cursor.next(false))
                this.done = true;
        }
    }
    hasNode(cursor) {
        this.moveTo(cursor.from);
        if (!this.done && this.cursor.from + this.offset == cursor.from && this.cursor.tree) {
            for (let tree = this.cursor.tree;;) {
                if (tree == cursor.tree)
                    return true;
                if (tree.children.length && tree.positions[0] == 0 && tree.children[0] instanceof Tree)
                    tree = tree.children[0];
                else
                    break;
            }
        }
        return false;
    }
}
class FragmentCursor {
    constructor(fragments) {
        var _a;
        this.fragments = fragments;
        this.curTo = 0;
        this.fragI = 0;
        if (fragments.length) {
            let first = this.curFrag = fragments[0];
            this.curTo = (_a = first.tree.prop(stoppedInner)) !== null && _a !== void 0 ? _a : first.to;
            this.inner = new StructureCursor(first.tree, -first.offset);
        }
        else {
            this.curFrag = this.inner = null;
        }
    }
    hasNode(node) {
        while (this.curFrag && node.from >= this.curTo)
            this.nextFrag();
        return this.curFrag && this.curFrag.from <= node.from && this.curTo >= node.to && this.inner.hasNode(node);
    }
    nextFrag() {
        var _a;
        this.fragI++;
        if (this.fragI == this.fragments.length) {
            this.curFrag = this.inner = null;
        }
        else {
            let frag = this.curFrag = this.fragments[this.fragI];
            this.curTo = (_a = frag.tree.prop(stoppedInner)) !== null && _a !== void 0 ? _a : frag.to;
            this.inner = new StructureCursor(frag.tree, -frag.offset);
        }
    }
    findMounts(pos, parser) {
        var _a;
        let result = [];
        if (this.inner) {
            this.inner.cursor.moveTo(pos, 1);
            for (let pos = this.inner.cursor.node; pos; pos = pos.parent) {
                let mount = (_a = pos.tree) === null || _a === void 0 ? void 0 : _a.prop(NodeProp.mounted);
                if (mount && mount.parser == parser) {
                    for (let i = this.fragI; i < this.fragments.length; i++) {
                        let frag = this.fragments[i];
                        if (frag.from >= pos.to)
                            break;
                        if (frag.tree == this.curFrag.tree)
                            result.push({
                                frag,
                                pos: pos.from - frag.offset,
                                mount
                            });
                    }
                }
            }
        }
        return result;
    }
}
function punchRanges(outer, ranges) {
    let copy = null, current = ranges;
    for (let i = 1, j = 0; i < outer.length; i++) {
        let gapFrom = outer[i - 1].to, gapTo = outer[i].from;
        for (; j < current.length; j++) {
            let r = current[j];
            if (r.from >= gapTo)
                break;
            if (r.to <= gapFrom)
                continue;
            if (!copy)
                current = copy = ranges.slice();
            if (r.from < gapFrom) {
                copy[j] = new Range(r.from, gapFrom);
                if (r.to > gapTo)
                    copy.splice(j + 1, 0, new Range(gapTo, r.to));
            }
            else if (r.to > gapTo) {
                copy[j--] = new Range(gapTo, r.to);
            }
            else {
                copy.splice(j--, 1);
            }
        }
    }
    return current;
}
function findCoverChanges(a, b, from, to) {
    let iA = 0, iB = 0, inA = false, inB = false, pos = -1e9;
    let result = [];
    for (;;) {
        let nextA = iA == a.length ? 1e9 : inA ? a[iA].to : a[iA].from;
        let nextB = iB == b.length ? 1e9 : inB ? b[iB].to : b[iB].from;
        if (inA != inB) {
            let start = Math.max(pos, from), end = Math.min(nextA, nextB, to);
            if (start < end)
                result.push(new Range(start, end));
        }
        pos = Math.min(nextA, nextB);
        if (pos == 1e9)
            break;
        if (nextA == pos) {
            if (!inA)
                inA = true;
            else {
                inA = false;
                iA++;
            }
        }
        if (nextB == pos) {
            if (!inB)
                inB = true;
            else {
                inB = false;
                iB++;
            }
        }
    }
    return result;
}
// Given a number of fragments for the outer tree, and a set of ranges
// to parse, find fragments for inner trees mounted around those
// ranges, if any.
function enterFragments(mounts, ranges) {
    let result = [];
    for (let { pos, mount, frag } of mounts) {
        let startPos = pos + (mount.overlay ? mount.overlay[0].from : 0), endPos = startPos + mount.tree.length;
        let from = Math.max(frag.from, startPos), to = Math.min(frag.to, endPos);
        if (mount.overlay) {
            let overlay = mount.overlay.map(r => new Range(r.from + pos, r.to + pos));
            let changes = findCoverChanges(ranges, overlay, from, to);
            for (let i = 0, pos = from;; i++) {
                let last = i == changes.length, end = last ? to : changes[i].from;
                if (end > pos)
                    result.push(new TreeFragment(pos, end, mount.tree, -startPos, frag.from >= pos || frag.openStart, frag.to <= end || frag.openEnd));
                if (last)
                    break;
                pos = changes[i].to;
            }
        }
        else {
            result.push(new TreeFragment(from, to, mount.tree, -startPos, frag.from >= startPos || frag.openStart, frag.to <= endPos || frag.openEnd));
        }
    }
    return result;
}




/***/ },

/***/ "../../.yarn/cache/@lezer-highlight-npm-1.2.3-e3bf6a2cc7-3bcb4fce7a.zip/node_modules/@lezer/highlight/dist/index.js"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   Tag: () => (/* binding */ Tag),
/* harmony export */   classHighlighter: () => (/* binding */ classHighlighter),
/* harmony export */   getStyleTags: () => (/* binding */ getStyleTags),
/* harmony export */   highlightCode: () => (/* binding */ highlightCode),
/* harmony export */   highlightTree: () => (/* binding */ highlightTree),
/* harmony export */   styleTags: () => (/* binding */ styleTags),
/* harmony export */   tagHighlighter: () => (/* binding */ tagHighlighter),
/* harmony export */   tags: () => (/* binding */ tags)
/* harmony export */ });
/* harmony import */ var _lezer_common__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("../../.yarn/cache/@lezer-common-npm-1.5.0-321d54f8ca-12c4b0ea9d.zip/node_modules/@lezer/common/dist/index.js");


let nextTagID = 0;
/**
Highlighting tags are markers that denote a highlighting category.
They are [associated](#highlight.styleTags) with parts of a syntax
tree by a language mode, and then mapped to an actual CSS style by
a [highlighter](#highlight.Highlighter).

Because syntax tree node types and highlight styles have to be
able to talk the same language, CodeMirror uses a mostly _closed_
[vocabulary](#highlight.tags) of syntax tags (as opposed to
traditional open string-based systems, which make it hard for
highlighting themes to cover all the tokens produced by the
various languages).

It _is_ possible to [define](#highlight.Tag^define) your own
highlighting tags for system-internal use (where you control both
the language package and the highlighter), but such tags will not
be picked up by regular highlighters (though you can derive them
from standard tags to allow highlighters to fall back to those).
*/
class Tag {
    /**
    @internal
    */
    constructor(
    /**
    The optional name of the base tag @internal
    */
    name, 
    /**
    The set of this tag and all its parent tags, starting with
    this one itself and sorted in order of decreasing specificity.
    */
    set, 
    /**
    The base unmodified tag that this one is based on, if it's
    modified @internal
    */
    base, 
    /**
    The modifiers applied to this.base @internal
    */
    modified) {
        this.name = name;
        this.set = set;
        this.base = base;
        this.modified = modified;
        /**
        @internal
        */
        this.id = nextTagID++;
    }
    toString() {
        let { name } = this;
        for (let mod of this.modified)
            if (mod.name)
                name = `${mod.name}(${name})`;
        return name;
    }
    static define(nameOrParent, parent) {
        let name = typeof nameOrParent == "string" ? nameOrParent : "?";
        if (nameOrParent instanceof Tag)
            parent = nameOrParent;
        if (parent === null || parent === void 0 ? void 0 : parent.base)
            throw new Error("Can not derive from a modified tag");
        let tag = new Tag(name, [], null, []);
        tag.set.push(tag);
        if (parent)
            for (let t of parent.set)
                tag.set.push(t);
        return tag;
    }
    /**
    Define a tag _modifier_, which is a function that, given a tag,
    will return a tag that is a subtag of the original. Applying the
    same modifier to a twice tag will return the same value (`m1(t1)
    == m1(t1)`) and applying multiple modifiers will, regardless or
    order, produce the same tag (`m1(m2(t1)) == m2(m1(t1))`).
    
    When multiple modifiers are applied to a given base tag, each
    smaller set of modifiers is registered as a parent, so that for
    example `m1(m2(m3(t1)))` is a subtype of `m1(m2(t1))`,
    `m1(m3(t1)`, and so on.
    */
    static defineModifier(name) {
        let mod = new Modifier(name);
        return (tag) => {
            if (tag.modified.indexOf(mod) > -1)
                return tag;
            return Modifier.get(tag.base || tag, tag.modified.concat(mod).sort((a, b) => a.id - b.id));
        };
    }
}
let nextModifierID = 0;
class Modifier {
    constructor(name) {
        this.name = name;
        this.instances = [];
        this.id = nextModifierID++;
    }
    static get(base, mods) {
        if (!mods.length)
            return base;
        let exists = mods[0].instances.find(t => t.base == base && sameArray(mods, t.modified));
        if (exists)
            return exists;
        let set = [], tag = new Tag(base.name, set, base, mods);
        for (let m of mods)
            m.instances.push(tag);
        let configs = powerSet(mods);
        for (let parent of base.set)
            if (!parent.modified.length)
                for (let config of configs)
                    set.push(Modifier.get(parent, config));
        return tag;
    }
}
function sameArray(a, b) {
    return a.length == b.length && a.every((x, i) => x == b[i]);
}
function powerSet(array) {
    let sets = [[]];
    for (let i = 0; i < array.length; i++) {
        for (let j = 0, e = sets.length; j < e; j++) {
            sets.push(sets[j].concat(array[i]));
        }
    }
    return sets.sort((a, b) => b.length - a.length);
}
/**
This function is used to add a set of tags to a language syntax
via [`NodeSet.extend`](#common.NodeSet.extend) or
[`LRParser.configure`](#lr.LRParser.configure).

The argument object maps node selectors to [highlighting
tags](#highlight.Tag) or arrays of tags.

Node selectors may hold one or more (space-separated) node paths.
Such a path can be a [node name](#common.NodeType.name), or
multiple node names (or `*` wildcards) separated by slash
characters, as in `"Block/Declaration/VariableName"`. Such a path
matches the final node but only if its direct parent nodes are the
other nodes mentioned. A `*` in such a path matches any parent,
but only a single level—wildcards that match multiple parents
aren't supported, both for efficiency reasons and because Lezer
trees make it rather hard to reason about what they would match.)

A path can be ended with `/...` to indicate that the tag assigned
to the node should also apply to all child nodes, even if they
match their own style (by default, only the innermost style is
used).

When a path ends in `!`, as in `Attribute!`, no further matching
happens for the node's child nodes, and the entire node gets the
given style.

In this notation, node names that contain `/`, `!`, `*`, or `...`
must be quoted as JSON strings.

For example:

```javascript
parser.configure({props: [
  styleTags({
    // Style Number and BigNumber nodes
    "Number BigNumber": tags.number,
    // Style Escape nodes whose parent is String
    "String/Escape": tags.escape,
    // Style anything inside Attributes nodes
    "Attributes!": tags.meta,
    // Add a style to all content inside Italic nodes
    "Italic/...": tags.emphasis,
    // Style InvalidString nodes as both `string` and `invalid`
    "InvalidString": [tags.string, tags.invalid],
    // Style the node named "/" as punctuation
    '"/"': tags.punctuation
  })
]})
```
*/
function styleTags(spec) {
    let byName = Object.create(null);
    for (let prop in spec) {
        let tags = spec[prop];
        if (!Array.isArray(tags))
            tags = [tags];
        for (let part of prop.split(" "))
            if (part) {
                let pieces = [], mode = 2 /* Mode.Normal */, rest = part;
                for (let pos = 0;;) {
                    if (rest == "..." && pos > 0 && pos + 3 == part.length) {
                        mode = 1 /* Mode.Inherit */;
                        break;
                    }
                    let m = /^"(?:[^"\\]|\\.)*?"|[^\/!]+/.exec(rest);
                    if (!m)
                        throw new RangeError("Invalid path: " + part);
                    pieces.push(m[0] == "*" ? "" : m[0][0] == '"' ? JSON.parse(m[0]) : m[0]);
                    pos += m[0].length;
                    if (pos == part.length)
                        break;
                    let next = part[pos++];
                    if (pos == part.length && next == "!") {
                        mode = 0 /* Mode.Opaque */;
                        break;
                    }
                    if (next != "/")
                        throw new RangeError("Invalid path: " + part);
                    rest = part.slice(pos);
                }
                let last = pieces.length - 1, inner = pieces[last];
                if (!inner)
                    throw new RangeError("Invalid path: " + part);
                let rule = new Rule(tags, mode, last > 0 ? pieces.slice(0, last) : null);
                byName[inner] = rule.sort(byName[inner]);
            }
    }
    return ruleNodeProp.add(byName);
}
const ruleNodeProp = new _lezer_common__WEBPACK_IMPORTED_MODULE_0__.NodeProp({
    combine(a, b) {
        let cur, root, take;
        while (a || b) {
            if (!a || b && a.depth >= b.depth) {
                take = b;
                b = b.next;
            }
            else {
                take = a;
                a = a.next;
            }
            if (cur && cur.mode == take.mode && !take.context && !cur.context)
                continue;
            let copy = new Rule(take.tags, take.mode, take.context);
            if (cur)
                cur.next = copy;
            else
                root = copy;
            cur = copy;
        }
        return root;
    }
});
class Rule {
    constructor(tags, mode, context, next) {
        this.tags = tags;
        this.mode = mode;
        this.context = context;
        this.next = next;
    }
    get opaque() { return this.mode == 0 /* Mode.Opaque */; }
    get inherit() { return this.mode == 1 /* Mode.Inherit */; }
    sort(other) {
        if (!other || other.depth < this.depth) {
            this.next = other;
            return this;
        }
        other.next = this.sort(other.next);
        return other;
    }
    get depth() { return this.context ? this.context.length : 0; }
}
Rule.empty = new Rule([], 2 /* Mode.Normal */, null);
/**
Define a [highlighter](#highlight.Highlighter) from an array of
tag/class pairs. Classes associated with more specific tags will
take precedence.
*/
function tagHighlighter(tags, options) {
    let map = Object.create(null);
    for (let style of tags) {
        if (!Array.isArray(style.tag))
            map[style.tag.id] = style.class;
        else
            for (let tag of style.tag)
                map[tag.id] = style.class;
    }
    let { scope, all = null } = options || {};
    return {
        style: (tags) => {
            let cls = all;
            for (let tag of tags) {
                for (let sub of tag.set) {
                    let tagClass = map[sub.id];
                    if (tagClass) {
                        cls = cls ? cls + " " + tagClass : tagClass;
                        break;
                    }
                }
            }
            return cls;
        },
        scope
    };
}
function highlightTags(highlighters, tags) {
    let result = null;
    for (let highlighter of highlighters) {
        let value = highlighter.style(tags);
        if (value)
            result = result ? result + " " + value : value;
    }
    return result;
}
/**
Highlight the given [tree](#common.Tree) with the given
[highlighter](#highlight.Highlighter). Often, the higher-level
[`highlightCode`](#highlight.highlightCode) function is easier to
use.
*/
function highlightTree(tree, highlighter, 
/**
Assign styling to a region of the text. Will be called, in order
of position, for any ranges where more than zero classes apply.
`classes` is a space separated string of CSS classes.
*/
putStyle, 
/**
The start of the range to highlight.
*/
from = 0, 
/**
The end of the range.
*/
to = tree.length) {
    let builder = new HighlightBuilder(from, Array.isArray(highlighter) ? highlighter : [highlighter], putStyle);
    builder.highlightRange(tree.cursor(), from, to, "", builder.highlighters);
    builder.flush(to);
}
/**
Highlight the given tree with the given highlighter, calling
`putText` for every piece of text, either with a set of classes or
with the empty string when unstyled, and `putBreak` for every line
break.
*/
function highlightCode(code, tree, highlighter, putText, putBreak, from = 0, to = code.length) {
    let pos = from;
    function writeTo(p, classes) {
        if (p <= pos)
            return;
        for (let text = code.slice(pos, p), i = 0;;) {
            let nextBreak = text.indexOf("\n", i);
            let upto = nextBreak < 0 ? text.length : nextBreak;
            if (upto > i)
                putText(text.slice(i, upto), classes);
            if (nextBreak < 0)
                break;
            putBreak();
            i = nextBreak + 1;
        }
        pos = p;
    }
    highlightTree(tree, highlighter, (from, to, classes) => {
        writeTo(from, "");
        writeTo(to, classes);
    }, from, to);
    writeTo(to, "");
}
class HighlightBuilder {
    constructor(at, highlighters, span) {
        this.at = at;
        this.highlighters = highlighters;
        this.span = span;
        this.class = "";
    }
    startSpan(at, cls) {
        if (cls != this.class) {
            this.flush(at);
            if (at > this.at)
                this.at = at;
            this.class = cls;
        }
    }
    flush(to) {
        if (to > this.at && this.class)
            this.span(this.at, to, this.class);
    }
    highlightRange(cursor, from, to, inheritedClass, highlighters) {
        let { type, from: start, to: end } = cursor;
        if (start >= to || end <= from)
            return;
        if (type.isTop)
            highlighters = this.highlighters.filter(h => !h.scope || h.scope(type));
        let cls = inheritedClass;
        let rule = getStyleTags(cursor) || Rule.empty;
        let tagCls = highlightTags(highlighters, rule.tags);
        if (tagCls) {
            if (cls)
                cls += " ";
            cls += tagCls;
            if (rule.mode == 1 /* Mode.Inherit */)
                inheritedClass += (inheritedClass ? " " : "") + tagCls;
        }
        this.startSpan(Math.max(from, start), cls);
        if (rule.opaque)
            return;
        let mounted = cursor.tree && cursor.tree.prop(_lezer_common__WEBPACK_IMPORTED_MODULE_0__.NodeProp.mounted);
        if (mounted && mounted.overlay) {
            let inner = cursor.node.enter(mounted.overlay[0].from + start, 1);
            let innerHighlighters = this.highlighters.filter(h => !h.scope || h.scope(mounted.tree.type));
            let hasChild = cursor.firstChild();
            for (let i = 0, pos = start;; i++) {
                let next = i < mounted.overlay.length ? mounted.overlay[i] : null;
                let nextPos = next ? next.from + start : end;
                let rangeFrom = Math.max(from, pos), rangeTo = Math.min(to, nextPos);
                if (rangeFrom < rangeTo && hasChild) {
                    while (cursor.from < rangeTo) {
                        this.highlightRange(cursor, rangeFrom, rangeTo, inheritedClass, highlighters);
                        this.startSpan(Math.min(rangeTo, cursor.to), cls);
                        if (cursor.to >= nextPos || !cursor.nextSibling())
                            break;
                    }
                }
                if (!next || nextPos > to)
                    break;
                pos = next.to + start;
                if (pos > from) {
                    this.highlightRange(inner.cursor(), Math.max(from, next.from + start), Math.min(to, pos), "", innerHighlighters);
                    this.startSpan(Math.min(to, pos), cls);
                }
            }
            if (hasChild)
                cursor.parent();
        }
        else if (cursor.firstChild()) {
            if (mounted)
                inheritedClass = "";
            do {
                if (cursor.to <= from)
                    continue;
                if (cursor.from >= to)
                    break;
                this.highlightRange(cursor, from, to, inheritedClass, highlighters);
                this.startSpan(Math.min(to, cursor.to), cls);
            } while (cursor.nextSibling());
            cursor.parent();
        }
    }
}
/**
Match a syntax node's [highlight rules](#highlight.styleTags). If
there's a match, return its set of tags, and whether it is
opaque (uses a `!`) or applies to all child nodes (`/...`).
*/
function getStyleTags(node) {
    let rule = node.type.prop(ruleNodeProp);
    while (rule && rule.context && !node.matchContext(rule.context))
        rule = rule.next;
    return rule || null;
}
const t = Tag.define;
const comment = t(), name = t(), typeName = t(name), propertyName = t(name), literal = t(), string = t(literal), number = t(literal), content = t(), heading = t(content), keyword = t(), operator = t(), punctuation = t(), bracket = t(punctuation), meta = t();
/**
The default set of highlighting [tags](#highlight.Tag).

This collection is heavily biased towards programming languages,
and necessarily incomplete. A full ontology of syntactic
constructs would fill a stack of books, and be impractical to
write themes for. So try to make do with this set. If all else
fails, [open an
issue](https://github.com/codemirror/codemirror.next) to propose a
new tag, or [define](#highlight.Tag^define) a local custom tag for
your use case.

Note that it is not obligatory to always attach the most specific
tag possible to an element—if your grammar can't easily
distinguish a certain type of element (such as a local variable),
it is okay to style it as its more general variant (a variable).

For tags that extend some parent tag, the documentation links to
the parent.
*/
const tags = {
    /**
    A comment.
    */
    comment,
    /**
    A line [comment](#highlight.tags.comment).
    */
    lineComment: t(comment),
    /**
    A block [comment](#highlight.tags.comment).
    */
    blockComment: t(comment),
    /**
    A documentation [comment](#highlight.tags.comment).
    */
    docComment: t(comment),
    /**
    Any kind of identifier.
    */
    name,
    /**
    The [name](#highlight.tags.name) of a variable.
    */
    variableName: t(name),
    /**
    A type [name](#highlight.tags.name).
    */
    typeName: typeName,
    /**
    A tag name (subtag of [`typeName`](#highlight.tags.typeName)).
    */
    tagName: t(typeName),
    /**
    A property or field [name](#highlight.tags.name).
    */
    propertyName: propertyName,
    /**
    An attribute name (subtag of [`propertyName`](#highlight.tags.propertyName)).
    */
    attributeName: t(propertyName),
    /**
    The [name](#highlight.tags.name) of a class.
    */
    className: t(name),
    /**
    A label [name](#highlight.tags.name).
    */
    labelName: t(name),
    /**
    A namespace [name](#highlight.tags.name).
    */
    namespace: t(name),
    /**
    The [name](#highlight.tags.name) of a macro.
    */
    macroName: t(name),
    /**
    A literal value.
    */
    literal,
    /**
    A string [literal](#highlight.tags.literal).
    */
    string,
    /**
    A documentation [string](#highlight.tags.string).
    */
    docString: t(string),
    /**
    A character literal (subtag of [string](#highlight.tags.string)).
    */
    character: t(string),
    /**
    An attribute value (subtag of [string](#highlight.tags.string)).
    */
    attributeValue: t(string),
    /**
    A number [literal](#highlight.tags.literal).
    */
    number,
    /**
    An integer [number](#highlight.tags.number) literal.
    */
    integer: t(number),
    /**
    A floating-point [number](#highlight.tags.number) literal.
    */
    float: t(number),
    /**
    A boolean [literal](#highlight.tags.literal).
    */
    bool: t(literal),
    /**
    Regular expression [literal](#highlight.tags.literal).
    */
    regexp: t(literal),
    /**
    An escape [literal](#highlight.tags.literal), for example a
    backslash escape in a string.
    */
    escape: t(literal),
    /**
    A color [literal](#highlight.tags.literal).
    */
    color: t(literal),
    /**
    A URL [literal](#highlight.tags.literal).
    */
    url: t(literal),
    /**
    A language keyword.
    */
    keyword,
    /**
    The [keyword](#highlight.tags.keyword) for the self or this
    object.
    */
    self: t(keyword),
    /**
    The [keyword](#highlight.tags.keyword) for null.
    */
    null: t(keyword),
    /**
    A [keyword](#highlight.tags.keyword) denoting some atomic value.
    */
    atom: t(keyword),
    /**
    A [keyword](#highlight.tags.keyword) that represents a unit.
    */
    unit: t(keyword),
    /**
    A modifier [keyword](#highlight.tags.keyword).
    */
    modifier: t(keyword),
    /**
    A [keyword](#highlight.tags.keyword) that acts as an operator.
    */
    operatorKeyword: t(keyword),
    /**
    A control-flow related [keyword](#highlight.tags.keyword).
    */
    controlKeyword: t(keyword),
    /**
    A [keyword](#highlight.tags.keyword) that defines something.
    */
    definitionKeyword: t(keyword),
    /**
    A [keyword](#highlight.tags.keyword) related to defining or
    interfacing with modules.
    */
    moduleKeyword: t(keyword),
    /**
    An operator.
    */
    operator,
    /**
    An [operator](#highlight.tags.operator) that dereferences something.
    */
    derefOperator: t(operator),
    /**
    Arithmetic-related [operator](#highlight.tags.operator).
    */
    arithmeticOperator: t(operator),
    /**
    Logical [operator](#highlight.tags.operator).
    */
    logicOperator: t(operator),
    /**
    Bit [operator](#highlight.tags.operator).
    */
    bitwiseOperator: t(operator),
    /**
    Comparison [operator](#highlight.tags.operator).
    */
    compareOperator: t(operator),
    /**
    [Operator](#highlight.tags.operator) that updates its operand.
    */
    updateOperator: t(operator),
    /**
    [Operator](#highlight.tags.operator) that defines something.
    */
    definitionOperator: t(operator),
    /**
    Type-related [operator](#highlight.tags.operator).
    */
    typeOperator: t(operator),
    /**
    Control-flow [operator](#highlight.tags.operator).
    */
    controlOperator: t(operator),
    /**
    Program or markup punctuation.
    */
    punctuation,
    /**
    [Punctuation](#highlight.tags.punctuation) that separates
    things.
    */
    separator: t(punctuation),
    /**
    Bracket-style [punctuation](#highlight.tags.punctuation).
    */
    bracket,
    /**
    Angle [brackets](#highlight.tags.bracket) (usually `<` and `>`
    tokens).
    */
    angleBracket: t(bracket),
    /**
    Square [brackets](#highlight.tags.bracket) (usually `[` and `]`
    tokens).
    */
    squareBracket: t(bracket),
    /**
    Parentheses (usually `(` and `)` tokens). Subtag of
    [bracket](#highlight.tags.bracket).
    */
    paren: t(bracket),
    /**
    Braces (usually `{` and `}` tokens). Subtag of
    [bracket](#highlight.tags.bracket).
    */
    brace: t(bracket),
    /**
    Content, for example plain text in XML or markup documents.
    */
    content,
    /**
    [Content](#highlight.tags.content) that represents a heading.
    */
    heading,
    /**
    A level 1 [heading](#highlight.tags.heading).
    */
    heading1: t(heading),
    /**
    A level 2 [heading](#highlight.tags.heading).
    */
    heading2: t(heading),
    /**
    A level 3 [heading](#highlight.tags.heading).
    */
    heading3: t(heading),
    /**
    A level 4 [heading](#highlight.tags.heading).
    */
    heading4: t(heading),
    /**
    A level 5 [heading](#highlight.tags.heading).
    */
    heading5: t(heading),
    /**
    A level 6 [heading](#highlight.tags.heading).
    */
    heading6: t(heading),
    /**
    A prose [content](#highlight.tags.content) separator (such as a horizontal rule).
    */
    contentSeparator: t(content),
    /**
    [Content](#highlight.tags.content) that represents a list.
    */
    list: t(content),
    /**
    [Content](#highlight.tags.content) that represents a quote.
    */
    quote: t(content),
    /**
    [Content](#highlight.tags.content) that is emphasized.
    */
    emphasis: t(content),
    /**
    [Content](#highlight.tags.content) that is styled strong.
    */
    strong: t(content),
    /**
    [Content](#highlight.tags.content) that is part of a link.
    */
    link: t(content),
    /**
    [Content](#highlight.tags.content) that is styled as code or
    monospace.
    */
    monospace: t(content),
    /**
    [Content](#highlight.tags.content) that has a strike-through
    style.
    */
    strikethrough: t(content),
    /**
    Inserted text in a change-tracking format.
    */
    inserted: t(),
    /**
    Deleted text.
    */
    deleted: t(),
    /**
    Changed text.
    */
    changed: t(),
    /**
    An invalid or unsyntactic element.
    */
    invalid: t(),
    /**
    Metadata or meta-instruction.
    */
    meta,
    /**
    [Metadata](#highlight.tags.meta) that applies to the entire
    document.
    */
    documentMeta: t(meta),
    /**
    [Metadata](#highlight.tags.meta) that annotates or adds
    attributes to a given syntactic element.
    */
    annotation: t(meta),
    /**
    Processing instruction or preprocessor directive. Subtag of
    [meta](#highlight.tags.meta).
    */
    processingInstruction: t(meta),
    /**
    [Modifier](#highlight.Tag^defineModifier) that indicates that a
    given element is being defined. Expected to be used with the
    various [name](#highlight.tags.name) tags.
    */
    definition: Tag.defineModifier("definition"),
    /**
    [Modifier](#highlight.Tag^defineModifier) that indicates that
    something is constant. Mostly expected to be used with
    [variable names](#highlight.tags.variableName).
    */
    constant: Tag.defineModifier("constant"),
    /**
    [Modifier](#highlight.Tag^defineModifier) used to indicate that
    a [variable](#highlight.tags.variableName) or [property
    name](#highlight.tags.propertyName) is being called or defined
    as a function.
    */
    function: Tag.defineModifier("function"),
    /**
    [Modifier](#highlight.Tag^defineModifier) that can be applied to
    [names](#highlight.tags.name) to indicate that they belong to
    the language's standard environment.
    */
    standard: Tag.defineModifier("standard"),
    /**
    [Modifier](#highlight.Tag^defineModifier) that indicates a given
    [names](#highlight.tags.name) is local to some scope.
    */
    local: Tag.defineModifier("local"),
    /**
    A generic variant [modifier](#highlight.Tag^defineModifier) that
    can be used to tag language-specific alternative variants of
    some common tag. It is recommended for themes to define special
    forms of at least the [string](#highlight.tags.string) and
    [variable name](#highlight.tags.variableName) tags, since those
    come up a lot.
    */
    special: Tag.defineModifier("special")
};
for (let name in tags) {
    let val = tags[name];
    if (val instanceof Tag)
        val.name = name;
}
/**
This is a highlighter that adds stable, predictable classes to
tokens, for styling with external CSS.

The following tags are mapped to their name prefixed with `"tok-"`
(for example `"tok-comment"`):

* [`link`](#highlight.tags.link)
* [`heading`](#highlight.tags.heading)
* [`emphasis`](#highlight.tags.emphasis)
* [`strong`](#highlight.tags.strong)
* [`keyword`](#highlight.tags.keyword)
* [`atom`](#highlight.tags.atom)
* [`bool`](#highlight.tags.bool)
* [`url`](#highlight.tags.url)
* [`labelName`](#highlight.tags.labelName)
* [`inserted`](#highlight.tags.inserted)
* [`deleted`](#highlight.tags.deleted)
* [`literal`](#highlight.tags.literal)
* [`string`](#highlight.tags.string)
* [`number`](#highlight.tags.number)
* [`variableName`](#highlight.tags.variableName)
* [`typeName`](#highlight.tags.typeName)
* [`namespace`](#highlight.tags.namespace)
* [`className`](#highlight.tags.className)
* [`macroName`](#highlight.tags.macroName)
* [`propertyName`](#highlight.tags.propertyName)
* [`operator`](#highlight.tags.operator)
* [`comment`](#highlight.tags.comment)
* [`meta`](#highlight.tags.meta)
* [`punctuation`](#highlight.tags.punctuation)
* [`invalid`](#highlight.tags.invalid)

In addition, these mappings are provided:

* [`regexp`](#highlight.tags.regexp),
  [`escape`](#highlight.tags.escape), and
  [`special`](#highlight.tags.special)[`(string)`](#highlight.tags.string)
  are mapped to `"tok-string2"`
* [`special`](#highlight.tags.special)[`(variableName)`](#highlight.tags.variableName)
  to `"tok-variableName2"`
* [`local`](#highlight.tags.local)[`(variableName)`](#highlight.tags.variableName)
  to `"tok-variableName tok-local"`
* [`definition`](#highlight.tags.definition)[`(variableName)`](#highlight.tags.variableName)
  to `"tok-variableName tok-definition"`
* [`definition`](#highlight.tags.definition)[`(propertyName)`](#highlight.tags.propertyName)
  to `"tok-propertyName tok-definition"`
*/
const classHighlighter = tagHighlighter([
    { tag: tags.link, class: "tok-link" },
    { tag: tags.heading, class: "tok-heading" },
    { tag: tags.emphasis, class: "tok-emphasis" },
    { tag: tags.strong, class: "tok-strong" },
    { tag: tags.keyword, class: "tok-keyword" },
    { tag: tags.atom, class: "tok-atom" },
    { tag: tags.bool, class: "tok-bool" },
    { tag: tags.url, class: "tok-url" },
    { tag: tags.labelName, class: "tok-labelName" },
    { tag: tags.inserted, class: "tok-inserted" },
    { tag: tags.deleted, class: "tok-deleted" },
    { tag: tags.literal, class: "tok-literal" },
    { tag: tags.string, class: "tok-string" },
    { tag: tags.number, class: "tok-number" },
    { tag: [tags.regexp, tags.escape, tags.special(tags.string)], class: "tok-string2" },
    { tag: tags.variableName, class: "tok-variableName" },
    { tag: tags.local(tags.variableName), class: "tok-variableName tok-local" },
    { tag: tags.definition(tags.variableName), class: "tok-variableName tok-definition" },
    { tag: tags.special(tags.variableName), class: "tok-variableName2" },
    { tag: tags.definition(tags.propertyName), class: "tok-propertyName tok-definition" },
    { tag: tags.typeName, class: "tok-typeName" },
    { tag: tags.namespace, class: "tok-namespace" },
    { tag: tags.className, class: "tok-className" },
    { tag: tags.macroName, class: "tok-macroName" },
    { tag: tags.propertyName, class: "tok-propertyName" },
    { tag: tags.operator, class: "tok-operator" },
    { tag: tags.comment, class: "tok-comment" },
    { tag: tags.meta, class: "tok-meta" },
    { tag: tags.invalid, class: "tok-invalid" },
    { tag: tags.punctuation, class: "tok-punctuation" }
]);




/***/ },

/***/ "../../.yarn/cache/@lezer-lr-npm-1.4.7-c15665133d-543c2e1aae.zip/node_modules/@lezer/lr/dist/index.js"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   ContextTracker: () => (/* binding */ ContextTracker),
/* harmony export */   ExternalTokenizer: () => (/* binding */ ExternalTokenizer),
/* harmony export */   InputStream: () => (/* binding */ InputStream),
/* harmony export */   LRParser: () => (/* binding */ LRParser),
/* harmony export */   LocalTokenGroup: () => (/* binding */ LocalTokenGroup),
/* harmony export */   Stack: () => (/* binding */ Stack)
/* harmony export */ });
/* harmony import */ var _lezer_common__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("../../.yarn/cache/@lezer-common-npm-1.5.0-321d54f8ca-12c4b0ea9d.zip/node_modules/@lezer/common/dist/index.js");


/**
A parse stack. These are used internally by the parser to track
parsing progress. They also provide some properties and methods
that external code such as a tokenizer can use to get information
about the parse state.
*/
class Stack {
    /**
    @internal
    */
    constructor(
    /**
    The parse that this stack is part of @internal
    */
    p, 
    /**
    Holds state, input pos, buffer index triplets for all but the
    top state @internal
    */
    stack, 
    /**
    The current parse state @internal
    */
    state, 
    // The position at which the next reduce should take place. This
    // can be less than `this.pos` when skipped expressions have been
    // added to the stack (which should be moved outside of the next
    // reduction)
    /**
    @internal
    */
    reducePos, 
    /**
    The input position up to which this stack has parsed.
    */
    pos, 
    /**
    The dynamic score of the stack, including dynamic precedence
    and error-recovery penalties
    @internal
    */
    score, 
    // The output buffer. Holds (type, start, end, size) quads
    // representing nodes created by the parser, where `size` is
    // amount of buffer array entries covered by this node.
    /**
    @internal
    */
    buffer, 
    // The base offset of the buffer. When stacks are split, the split
    // instance shared the buffer history with its parent up to
    // `bufferBase`, which is the absolute offset (including the
    // offset of previous splits) into the buffer at which this stack
    // starts writing.
    /**
    @internal
    */
    bufferBase, 
    /**
    @internal
    */
    curContext, 
    /**
    @internal
    */
    lookAhead = 0, 
    // A parent stack from which this was split off, if any. This is
    // set up so that it always points to a stack that has some
    // additional buffer content, never to a stack with an equal
    // `bufferBase`.
    /**
    @internal
    */
    parent) {
        this.p = p;
        this.stack = stack;
        this.state = state;
        this.reducePos = reducePos;
        this.pos = pos;
        this.score = score;
        this.buffer = buffer;
        this.bufferBase = bufferBase;
        this.curContext = curContext;
        this.lookAhead = lookAhead;
        this.parent = parent;
    }
    /**
    @internal
    */
    toString() {
        return `[${this.stack.filter((_, i) => i % 3 == 0).concat(this.state)}]@${this.pos}${this.score ? "!" + this.score : ""}`;
    }
    // Start an empty stack
    /**
    @internal
    */
    static start(p, state, pos = 0) {
        let cx = p.parser.context;
        return new Stack(p, [], state, pos, pos, 0, [], 0, cx ? new StackContext(cx, cx.start) : null, 0, null);
    }
    /**
    The stack's current [context](#lr.ContextTracker) value, if
    any. Its type will depend on the context tracker's type
    parameter, or it will be `null` if there is no context
    tracker.
    */
    get context() { return this.curContext ? this.curContext.context : null; }
    // Push a state onto the stack, tracking its start position as well
    // as the buffer base at that point.
    /**
    @internal
    */
    pushState(state, start) {
        this.stack.push(this.state, start, this.bufferBase + this.buffer.length);
        this.state = state;
    }
    // Apply a reduce action
    /**
    @internal
    */
    reduce(action) {
        var _a;
        let depth = action >> 19 /* Action.ReduceDepthShift */, type = action & 65535 /* Action.ValueMask */;
        let { parser } = this.p;
        let lookaheadRecord = this.reducePos < this.pos - 25 /* Lookahead.Margin */ && this.setLookAhead(this.pos);
        let dPrec = parser.dynamicPrecedence(type);
        if (dPrec)
            this.score += dPrec;
        if (depth == 0) {
            this.pushState(parser.getGoto(this.state, type, true), this.reducePos);
            // Zero-depth reductions are a special case—they add stuff to
            // the stack without popping anything off.
            if (type < parser.minRepeatTerm)
                this.storeNode(type, this.reducePos, this.reducePos, lookaheadRecord ? 8 : 4, true);
            this.reduceContext(type, this.reducePos);
            return;
        }
        // Find the base index into `this.stack`, content after which will
        // be dropped. Note that with `StayFlag` reductions we need to
        // consume two extra frames (the dummy parent node for the skipped
        // expression and the state that we'll be staying in, which should
        // be moved to `this.state`).
        let base = this.stack.length - ((depth - 1) * 3) - (action & 262144 /* Action.StayFlag */ ? 6 : 0);
        let start = base ? this.stack[base - 2] : this.p.ranges[0].from, size = this.reducePos - start;
        // This is a kludge to try and detect overly deep left-associative
        // trees, which will not increase the parse stack depth and thus
        // won't be caught by the regular stack-depth limit check.
        if (size >= 2000 /* Recover.MinBigReduction */ && !((_a = this.p.parser.nodeSet.types[type]) === null || _a === void 0 ? void 0 : _a.isAnonymous)) {
            if (start == this.p.lastBigReductionStart) {
                this.p.bigReductionCount++;
                this.p.lastBigReductionSize = size;
            }
            else if (this.p.lastBigReductionSize < size) {
                this.p.bigReductionCount = 1;
                this.p.lastBigReductionStart = start;
                this.p.lastBigReductionSize = size;
            }
        }
        let bufferBase = base ? this.stack[base - 1] : 0, count = this.bufferBase + this.buffer.length - bufferBase;
        // Store normal terms or `R -> R R` repeat reductions
        if (type < parser.minRepeatTerm || (action & 131072 /* Action.RepeatFlag */)) {
            let pos = parser.stateFlag(this.state, 1 /* StateFlag.Skipped */) ? this.pos : this.reducePos;
            this.storeNode(type, start, pos, count + 4, true);
        }
        if (action & 262144 /* Action.StayFlag */) {
            this.state = this.stack[base];
        }
        else {
            let baseStateID = this.stack[base - 3];
            this.state = parser.getGoto(baseStateID, type, true);
        }
        while (this.stack.length > base)
            this.stack.pop();
        this.reduceContext(type, start);
    }
    // Shift a value into the buffer
    /**
    @internal
    */
    storeNode(term, start, end, size = 4, mustSink = false) {
        if (term == 0 /* Term.Err */ &&
            (!this.stack.length || this.stack[this.stack.length - 1] < this.buffer.length + this.bufferBase)) {
            // Try to omit/merge adjacent error nodes
            let cur = this, top = this.buffer.length;
            if (top == 0 && cur.parent) {
                top = cur.bufferBase - cur.parent.bufferBase;
                cur = cur.parent;
            }
            if (top > 0 && cur.buffer[top - 4] == 0 /* Term.Err */ && cur.buffer[top - 1] > -1) {
                if (start == end)
                    return;
                if (cur.buffer[top - 2] >= start) {
                    cur.buffer[top - 2] = end;
                    return;
                }
            }
        }
        if (!mustSink || this.pos == end) { // Simple case, just append
            this.buffer.push(term, start, end, size);
        }
        else { // There may be skipped nodes that have to be moved forward
            let index = this.buffer.length;
            if (index > 0 && (this.buffer[index - 4] != 0 /* Term.Err */ || this.buffer[index - 1] < 0)) {
                let mustMove = false;
                for (let scan = index; scan > 0 && this.buffer[scan - 2] > end; scan -= 4) {
                    if (this.buffer[scan - 1] >= 0) {
                        mustMove = true;
                        break;
                    }
                }
                if (mustMove)
                    while (index > 0 && this.buffer[index - 2] > end) {
                        // Move this record forward
                        this.buffer[index] = this.buffer[index - 4];
                        this.buffer[index + 1] = this.buffer[index - 3];
                        this.buffer[index + 2] = this.buffer[index - 2];
                        this.buffer[index + 3] = this.buffer[index - 1];
                        index -= 4;
                        if (size > 4)
                            size -= 4;
                    }
            }
            this.buffer[index] = term;
            this.buffer[index + 1] = start;
            this.buffer[index + 2] = end;
            this.buffer[index + 3] = size;
        }
    }
    // Apply a shift action
    /**
    @internal
    */
    shift(action, type, start, end) {
        if (action & 131072 /* Action.GotoFlag */) {
            this.pushState(action & 65535 /* Action.ValueMask */, this.pos);
        }
        else if ((action & 262144 /* Action.StayFlag */) == 0) { // Regular shift
            let nextState = action, { parser } = this.p;
            this.pos = end;
            // Skipped or zero-length non-tree tokens don't move reducePos
            if (!parser.stateFlag(nextState, 1 /* StateFlag.Skipped */) && (end > start || type <= parser.maxNode))
                this.reducePos = end;
            this.pushState(nextState, Math.min(start, this.reducePos));
            this.shiftContext(type, start);
            if (type <= parser.maxNode)
                this.buffer.push(type, start, end, 4);
        }
        else { // Shift-and-stay, which means this is a skipped token
            this.pos = end;
            this.shiftContext(type, start);
            if (type <= this.p.parser.maxNode)
                this.buffer.push(type, start, end, 4);
        }
    }
    // Apply an action
    /**
    @internal
    */
    apply(action, next, nextStart, nextEnd) {
        if (action & 65536 /* Action.ReduceFlag */)
            this.reduce(action);
        else
            this.shift(action, next, nextStart, nextEnd);
    }
    // Add a prebuilt (reused) node into the buffer.
    /**
    @internal
    */
    useNode(value, next) {
        let index = this.p.reused.length - 1;
        if (index < 0 || this.p.reused[index] != value) {
            this.p.reused.push(value);
            index++;
        }
        let start = this.pos;
        this.reducePos = this.pos = start + value.length;
        this.pushState(next, start);
        this.buffer.push(index, start, this.reducePos, -1 /* size == -1 means this is a reused value */);
        if (this.curContext)
            this.updateContext(this.curContext.tracker.reuse(this.curContext.context, value, this, this.p.stream.reset(this.pos - value.length)));
    }
    // Split the stack. Due to the buffer sharing and the fact
    // that `this.stack` tends to stay quite shallow, this isn't very
    // expensive.
    /**
    @internal
    */
    split() {
        let parent = this;
        let off = parent.buffer.length;
        // Because the top of the buffer (after this.pos) may be mutated
        // to reorder reductions and skipped tokens, and shared buffers
        // should be immutable, this copies any outstanding skipped tokens
        // to the new buffer, and puts the base pointer before them.
        while (off > 0 && parent.buffer[off - 2] > parent.reducePos)
            off -= 4;
        let buffer = parent.buffer.slice(off), base = parent.bufferBase + off;
        // Make sure parent points to an actual parent with content, if there is such a parent.
        while (parent && base == parent.bufferBase)
            parent = parent.parent;
        return new Stack(this.p, this.stack.slice(), this.state, this.reducePos, this.pos, this.score, buffer, base, this.curContext, this.lookAhead, parent);
    }
    // Try to recover from an error by 'deleting' (ignoring) one token.
    /**
    @internal
    */
    recoverByDelete(next, nextEnd) {
        let isNode = next <= this.p.parser.maxNode;
        if (isNode)
            this.storeNode(next, this.pos, nextEnd, 4);
        this.storeNode(0 /* Term.Err */, this.pos, nextEnd, isNode ? 8 : 4);
        this.pos = this.reducePos = nextEnd;
        this.score -= 190 /* Recover.Delete */;
    }
    /**
    Check if the given term would be able to be shifted (optionally
    after some reductions) on this stack. This can be useful for
    external tokenizers that want to make sure they only provide a
    given token when it applies.
    */
    canShift(term) {
        for (let sim = new SimulatedStack(this);;) {
            let action = this.p.parser.stateSlot(sim.state, 4 /* ParseState.DefaultReduce */) || this.p.parser.hasAction(sim.state, term);
            if (action == 0)
                return false;
            if ((action & 65536 /* Action.ReduceFlag */) == 0)
                return true;
            sim.reduce(action);
        }
    }
    // Apply up to Recover.MaxNext recovery actions that conceptually
    // inserts some missing token or rule.
    /**
    @internal
    */
    recoverByInsert(next) {
        if (this.stack.length >= 300 /* Recover.MaxInsertStackDepth */)
            return [];
        let nextStates = this.p.parser.nextStates(this.state);
        if (nextStates.length > 4 /* Recover.MaxNext */ << 1 || this.stack.length >= 120 /* Recover.DampenInsertStackDepth */) {
            let best = [];
            for (let i = 0, s; i < nextStates.length; i += 2) {
                if ((s = nextStates[i + 1]) != this.state && this.p.parser.hasAction(s, next))
                    best.push(nextStates[i], s);
            }
            if (this.stack.length < 120 /* Recover.DampenInsertStackDepth */)
                for (let i = 0; best.length < 4 /* Recover.MaxNext */ << 1 && i < nextStates.length; i += 2) {
                    let s = nextStates[i + 1];
                    if (!best.some((v, i) => (i & 1) && v == s))
                        best.push(nextStates[i], s);
                }
            nextStates = best;
        }
        let result = [];
        for (let i = 0; i < nextStates.length && result.length < 4 /* Recover.MaxNext */; i += 2) {
            let s = nextStates[i + 1];
            if (s == this.state)
                continue;
            let stack = this.split();
            stack.pushState(s, this.pos);
            stack.storeNode(0 /* Term.Err */, stack.pos, stack.pos, 4, true);
            stack.shiftContext(nextStates[i], this.pos);
            stack.reducePos = this.pos;
            stack.score -= 200 /* Recover.Insert */;
            result.push(stack);
        }
        return result;
    }
    // Force a reduce, if possible. Return false if that can't
    // be done.
    /**
    @internal
    */
    forceReduce() {
        let { parser } = this.p;
        let reduce = parser.stateSlot(this.state, 5 /* ParseState.ForcedReduce */);
        if ((reduce & 65536 /* Action.ReduceFlag */) == 0)
            return false;
        if (!parser.validAction(this.state, reduce)) {
            let depth = reduce >> 19 /* Action.ReduceDepthShift */, term = reduce & 65535 /* Action.ValueMask */;
            let target = this.stack.length - depth * 3;
            if (target < 0 || parser.getGoto(this.stack[target], term, false) < 0) {
                let backup = this.findForcedReduction();
                if (backup == null)
                    return false;
                reduce = backup;
            }
            this.storeNode(0 /* Term.Err */, this.pos, this.pos, 4, true);
            this.score -= 100 /* Recover.Reduce */;
        }
        this.reducePos = this.pos;
        this.reduce(reduce);
        return true;
    }
    /**
    Try to scan through the automaton to find some kind of reduction
    that can be applied. Used when the regular ForcedReduce field
    isn't a valid action. @internal
    */
    findForcedReduction() {
        let { parser } = this.p, seen = [];
        let explore = (state, depth) => {
            if (seen.includes(state))
                return;
            seen.push(state);
            return parser.allActions(state, (action) => {
                if (action & (262144 /* Action.StayFlag */ | 131072 /* Action.GotoFlag */)) ;
                else if (action & 65536 /* Action.ReduceFlag */) {
                    let rDepth = (action >> 19 /* Action.ReduceDepthShift */) - depth;
                    if (rDepth > 1) {
                        let term = action & 65535 /* Action.ValueMask */, target = this.stack.length - rDepth * 3;
                        if (target >= 0 && parser.getGoto(this.stack[target], term, false) >= 0)
                            return (rDepth << 19 /* Action.ReduceDepthShift */) | 65536 /* Action.ReduceFlag */ | term;
                    }
                }
                else {
                    let found = explore(action, depth + 1);
                    if (found != null)
                        return found;
                }
            });
        };
        return explore(this.state, 0);
    }
    /**
    @internal
    */
    forceAll() {
        while (!this.p.parser.stateFlag(this.state, 2 /* StateFlag.Accepting */)) {
            if (!this.forceReduce()) {
                this.storeNode(0 /* Term.Err */, this.pos, this.pos, 4, true);
                break;
            }
        }
        return this;
    }
    /**
    Check whether this state has no further actions (assumed to be a direct descendant of the
    top state, since any other states must be able to continue
    somehow). @internal
    */
    get deadEnd() {
        if (this.stack.length != 3)
            return false;
        let { parser } = this.p;
        return parser.data[parser.stateSlot(this.state, 1 /* ParseState.Actions */)] == 65535 /* Seq.End */ &&
            !parser.stateSlot(this.state, 4 /* ParseState.DefaultReduce */);
    }
    /**
    Restart the stack (put it back in its start state). Only safe
    when this.stack.length == 3 (state is directly below the top
    state). @internal
    */
    restart() {
        this.storeNode(0 /* Term.Err */, this.pos, this.pos, 4, true);
        this.state = this.stack[0];
        this.stack.length = 0;
    }
    /**
    @internal
    */
    sameState(other) {
        if (this.state != other.state || this.stack.length != other.stack.length)
            return false;
        for (let i = 0; i < this.stack.length; i += 3)
            if (this.stack[i] != other.stack[i])
                return false;
        return true;
    }
    /**
    Get the parser used by this stack.
    */
    get parser() { return this.p.parser; }
    /**
    Test whether a given dialect (by numeric ID, as exported from
    the terms file) is enabled.
    */
    dialectEnabled(dialectID) { return this.p.parser.dialect.flags[dialectID]; }
    shiftContext(term, start) {
        if (this.curContext)
            this.updateContext(this.curContext.tracker.shift(this.curContext.context, term, this, this.p.stream.reset(start)));
    }
    reduceContext(term, start) {
        if (this.curContext)
            this.updateContext(this.curContext.tracker.reduce(this.curContext.context, term, this, this.p.stream.reset(start)));
    }
    /**
    @internal
    */
    emitContext() {
        let last = this.buffer.length - 1;
        if (last < 0 || this.buffer[last] != -3)
            this.buffer.push(this.curContext.hash, this.pos, this.pos, -3);
    }
    /**
    @internal
    */
    emitLookAhead() {
        let last = this.buffer.length - 1;
        if (last < 0 || this.buffer[last] != -4)
            this.buffer.push(this.lookAhead, this.pos, this.pos, -4);
    }
    updateContext(context) {
        if (context != this.curContext.context) {
            let newCx = new StackContext(this.curContext.tracker, context);
            if (newCx.hash != this.curContext.hash)
                this.emitContext();
            this.curContext = newCx;
        }
    }
    /**
    @internal
    */
    setLookAhead(lookAhead) {
        if (lookAhead <= this.lookAhead)
            return false;
        this.emitLookAhead();
        this.lookAhead = lookAhead;
        return true;
    }
    /**
    @internal
    */
    close() {
        if (this.curContext && this.curContext.tracker.strict)
            this.emitContext();
        if (this.lookAhead > 0)
            this.emitLookAhead();
    }
}
class StackContext {
    constructor(tracker, context) {
        this.tracker = tracker;
        this.context = context;
        this.hash = tracker.strict ? tracker.hash(context) : 0;
    }
}
// Used to cheaply run some reductions to scan ahead without mutating
// an entire stack
class SimulatedStack {
    constructor(start) {
        this.start = start;
        this.state = start.state;
        this.stack = start.stack;
        this.base = this.stack.length;
    }
    reduce(action) {
        let term = action & 65535 /* Action.ValueMask */, depth = action >> 19 /* Action.ReduceDepthShift */;
        if (depth == 0) {
            if (this.stack == this.start.stack)
                this.stack = this.stack.slice();
            this.stack.push(this.state, 0, 0);
            this.base += 3;
        }
        else {
            this.base -= (depth - 1) * 3;
        }
        let goto = this.start.p.parser.getGoto(this.stack[this.base - 3], term, true);
        this.state = goto;
    }
}
// This is given to `Tree.build` to build a buffer, and encapsulates
// the parent-stack-walking necessary to read the nodes.
class StackBufferCursor {
    constructor(stack, pos, index) {
        this.stack = stack;
        this.pos = pos;
        this.index = index;
        this.buffer = stack.buffer;
        if (this.index == 0)
            this.maybeNext();
    }
    static create(stack, pos = stack.bufferBase + stack.buffer.length) {
        return new StackBufferCursor(stack, pos, pos - stack.bufferBase);
    }
    maybeNext() {
        let next = this.stack.parent;
        if (next != null) {
            this.index = this.stack.bufferBase - next.bufferBase;
            this.stack = next;
            this.buffer = next.buffer;
        }
    }
    get id() { return this.buffer[this.index - 4]; }
    get start() { return this.buffer[this.index - 3]; }
    get end() { return this.buffer[this.index - 2]; }
    get size() { return this.buffer[this.index - 1]; }
    next() {
        this.index -= 4;
        this.pos -= 4;
        if (this.index == 0)
            this.maybeNext();
    }
    fork() {
        return new StackBufferCursor(this.stack, this.pos, this.index);
    }
}

// See lezer-generator/src/encode.ts for comments about the encoding
// used here
function decodeArray(input, Type = Uint16Array) {
    if (typeof input != "string")
        return input;
    let array = null;
    for (let pos = 0, out = 0; pos < input.length;) {
        let value = 0;
        for (;;) {
            let next = input.charCodeAt(pos++), stop = false;
            if (next == 126 /* Encode.BigValCode */) {
                value = 65535 /* Encode.BigVal */;
                break;
            }
            if (next >= 92 /* Encode.Gap2 */)
                next--;
            if (next >= 34 /* Encode.Gap1 */)
                next--;
            let digit = next - 32 /* Encode.Start */;
            if (digit >= 46 /* Encode.Base */) {
                digit -= 46 /* Encode.Base */;
                stop = true;
            }
            value += digit;
            if (stop)
                break;
            value *= 46 /* Encode.Base */;
        }
        if (array)
            array[out++] = value;
        else
            array = new Type(value);
    }
    return array;
}

class CachedToken {
    constructor() {
        this.start = -1;
        this.value = -1;
        this.end = -1;
        this.extended = -1;
        this.lookAhead = 0;
        this.mask = 0;
        this.context = 0;
    }
}
const nullToken = new CachedToken;
/**
[Tokenizers](#lr.ExternalTokenizer) interact with the input
through this interface. It presents the input as a stream of
characters, tracking lookahead and hiding the complexity of
[ranges](#common.Parser.parse^ranges) from tokenizer code.
*/
class InputStream {
    /**
    @internal
    */
    constructor(
    /**
    @internal
    */
    input, 
    /**
    @internal
    */
    ranges) {
        this.input = input;
        this.ranges = ranges;
        /**
        @internal
        */
        this.chunk = "";
        /**
        @internal
        */
        this.chunkOff = 0;
        /**
        Backup chunk
        */
        this.chunk2 = "";
        this.chunk2Pos = 0;
        /**
        The character code of the next code unit in the input, or -1
        when the stream is at the end of the input.
        */
        this.next = -1;
        /**
        @internal
        */
        this.token = nullToken;
        this.rangeIndex = 0;
        this.pos = this.chunkPos = ranges[0].from;
        this.range = ranges[0];
        this.end = ranges[ranges.length - 1].to;
        this.readNext();
    }
    /**
    @internal
    */
    resolveOffset(offset, assoc) {
        let range = this.range, index = this.rangeIndex;
        let pos = this.pos + offset;
        while (pos < range.from) {
            if (!index)
                return null;
            let next = this.ranges[--index];
            pos -= range.from - next.to;
            range = next;
        }
        while (assoc < 0 ? pos > range.to : pos >= range.to) {
            if (index == this.ranges.length - 1)
                return null;
            let next = this.ranges[++index];
            pos += next.from - range.to;
            range = next;
        }
        return pos;
    }
    /**
    @internal
    */
    clipPos(pos) {
        if (pos >= this.range.from && pos < this.range.to)
            return pos;
        for (let range of this.ranges)
            if (range.to > pos)
                return Math.max(pos, range.from);
        return this.end;
    }
    /**
    Look at a code unit near the stream position. `.peek(0)` equals
    `.next`, `.peek(-1)` gives you the previous character, and so
    on.
    
    Note that looking around during tokenizing creates dependencies
    on potentially far-away content, which may reduce the
    effectiveness incremental parsing—when looking forward—or even
    cause invalid reparses when looking backward more than 25 code
    units, since the library does not track lookbehind.
    */
    peek(offset) {
        let idx = this.chunkOff + offset, pos, result;
        if (idx >= 0 && idx < this.chunk.length) {
            pos = this.pos + offset;
            result = this.chunk.charCodeAt(idx);
        }
        else {
            let resolved = this.resolveOffset(offset, 1);
            if (resolved == null)
                return -1;
            pos = resolved;
            if (pos >= this.chunk2Pos && pos < this.chunk2Pos + this.chunk2.length) {
                result = this.chunk2.charCodeAt(pos - this.chunk2Pos);
            }
            else {
                let i = this.rangeIndex, range = this.range;
                while (range.to <= pos)
                    range = this.ranges[++i];
                this.chunk2 = this.input.chunk(this.chunk2Pos = pos);
                if (pos + this.chunk2.length > range.to)
                    this.chunk2 = this.chunk2.slice(0, range.to - pos);
                result = this.chunk2.charCodeAt(0);
            }
        }
        if (pos >= this.token.lookAhead)
            this.token.lookAhead = pos + 1;
        return result;
    }
    /**
    Accept a token. By default, the end of the token is set to the
    current stream position, but you can pass an offset (relative to
    the stream position) to change that.
    */
    acceptToken(token, endOffset = 0) {
        let end = endOffset ? this.resolveOffset(endOffset, -1) : this.pos;
        if (end == null || end < this.token.start)
            throw new RangeError("Token end out of bounds");
        this.token.value = token;
        this.token.end = end;
    }
    /**
    Accept a token ending at a specific given position.
    */
    acceptTokenTo(token, endPos) {
        this.token.value = token;
        this.token.end = endPos;
    }
    getChunk() {
        if (this.pos >= this.chunk2Pos && this.pos < this.chunk2Pos + this.chunk2.length) {
            let { chunk, chunkPos } = this;
            this.chunk = this.chunk2;
            this.chunkPos = this.chunk2Pos;
            this.chunk2 = chunk;
            this.chunk2Pos = chunkPos;
            this.chunkOff = this.pos - this.chunkPos;
        }
        else {
            this.chunk2 = this.chunk;
            this.chunk2Pos = this.chunkPos;
            let nextChunk = this.input.chunk(this.pos);
            let end = this.pos + nextChunk.length;
            this.chunk = end > this.range.to ? nextChunk.slice(0, this.range.to - this.pos) : nextChunk;
            this.chunkPos = this.pos;
            this.chunkOff = 0;
        }
    }
    readNext() {
        if (this.chunkOff >= this.chunk.length) {
            this.getChunk();
            if (this.chunkOff == this.chunk.length)
                return this.next = -1;
        }
        return this.next = this.chunk.charCodeAt(this.chunkOff);
    }
    /**
    Move the stream forward N (defaults to 1) code units. Returns
    the new value of [`next`](#lr.InputStream.next).
    */
    advance(n = 1) {
        this.chunkOff += n;
        while (this.pos + n >= this.range.to) {
            if (this.rangeIndex == this.ranges.length - 1)
                return this.setDone();
            n -= this.range.to - this.pos;
            this.range = this.ranges[++this.rangeIndex];
            this.pos = this.range.from;
        }
        this.pos += n;
        if (this.pos >= this.token.lookAhead)
            this.token.lookAhead = this.pos + 1;
        return this.readNext();
    }
    setDone() {
        this.pos = this.chunkPos = this.end;
        this.range = this.ranges[this.rangeIndex = this.ranges.length - 1];
        this.chunk = "";
        return this.next = -1;
    }
    /**
    @internal
    */
    reset(pos, token) {
        if (token) {
            this.token = token;
            token.start = pos;
            token.lookAhead = pos + 1;
            token.value = token.extended = -1;
        }
        else {
            this.token = nullToken;
        }
        if (this.pos != pos) {
            this.pos = pos;
            if (pos == this.end) {
                this.setDone();
                return this;
            }
            while (pos < this.range.from)
                this.range = this.ranges[--this.rangeIndex];
            while (pos >= this.range.to)
                this.range = this.ranges[++this.rangeIndex];
            if (pos >= this.chunkPos && pos < this.chunkPos + this.chunk.length) {
                this.chunkOff = pos - this.chunkPos;
            }
            else {
                this.chunk = "";
                this.chunkOff = 0;
            }
            this.readNext();
        }
        return this;
    }
    /**
    @internal
    */
    read(from, to) {
        if (from >= this.chunkPos && to <= this.chunkPos + this.chunk.length)
            return this.chunk.slice(from - this.chunkPos, to - this.chunkPos);
        if (from >= this.chunk2Pos && to <= this.chunk2Pos + this.chunk2.length)
            return this.chunk2.slice(from - this.chunk2Pos, to - this.chunk2Pos);
        if (from >= this.range.from && to <= this.range.to)
            return this.input.read(from, to);
        let result = "";
        for (let r of this.ranges) {
            if (r.from >= to)
                break;
            if (r.to > from)
                result += this.input.read(Math.max(r.from, from), Math.min(r.to, to));
        }
        return result;
    }
}
/**
@internal
*/
class TokenGroup {
    constructor(data, id) {
        this.data = data;
        this.id = id;
    }
    token(input, stack) {
        let { parser } = stack.p;
        readToken(this.data, input, stack, this.id, parser.data, parser.tokenPrecTable);
    }
}
TokenGroup.prototype.contextual = TokenGroup.prototype.fallback = TokenGroup.prototype.extend = false;
/**
@hide
*/
class LocalTokenGroup {
    constructor(data, precTable, elseToken) {
        this.precTable = precTable;
        this.elseToken = elseToken;
        this.data = typeof data == "string" ? decodeArray(data) : data;
    }
    token(input, stack) {
        let start = input.pos, skipped = 0;
        for (;;) {
            let atEof = input.next < 0, nextPos = input.resolveOffset(1, 1);
            readToken(this.data, input, stack, 0, this.data, this.precTable);
            if (input.token.value > -1)
                break;
            if (this.elseToken == null)
                return;
            if (!atEof)
                skipped++;
            if (nextPos == null)
                break;
            input.reset(nextPos, input.token);
        }
        if (skipped) {
            input.reset(start, input.token);
            input.acceptToken(this.elseToken, skipped);
        }
    }
}
LocalTokenGroup.prototype.contextual = TokenGroup.prototype.fallback = TokenGroup.prototype.extend = false;
/**
`@external tokens` declarations in the grammar should resolve to
an instance of this class.
*/
class ExternalTokenizer {
    /**
    Create a tokenizer. The first argument is the function that,
    given an input stream, scans for the types of tokens it
    recognizes at the stream's position, and calls
    [`acceptToken`](#lr.InputStream.acceptToken) when it finds
    one.
    */
    constructor(
    /**
    @internal
    */
    token, options = {}) {
        this.token = token;
        this.contextual = !!options.contextual;
        this.fallback = !!options.fallback;
        this.extend = !!options.extend;
    }
}
// Tokenizer data is stored a big uint16 array containing, for each
// state:
//
//  - A group bitmask, indicating what token groups are reachable from
//    this state, so that paths that can only lead to tokens not in
//    any of the current groups can be cut off early.
//
//  - The position of the end of the state's sequence of accepting
//    tokens
//
//  - The number of outgoing edges for the state
//
//  - The accepting tokens, as (token id, group mask) pairs
//
//  - The outgoing edges, as (start character, end character, state
//    index) triples, with end character being exclusive
//
// This function interprets that data, running through a stream as
// long as new states with the a matching group mask can be reached,
// and updating `input.token` when it matches a token.
function readToken(data, input, stack, group, precTable, precOffset) {
    let state = 0, groupMask = 1 << group, { dialect } = stack.p.parser;
    scan: for (;;) {
        if ((groupMask & data[state]) == 0)
            break;
        let accEnd = data[state + 1];
        // Check whether this state can lead to a token in the current group
        // Accept tokens in this state, possibly overwriting
        // lower-precedence / shorter tokens
        for (let i = state + 3; i < accEnd; i += 2)
            if ((data[i + 1] & groupMask) > 0) {
                let term = data[i];
                if (dialect.allows(term) &&
                    (input.token.value == -1 || input.token.value == term ||
                        overrides(term, input.token.value, precTable, precOffset))) {
                    input.acceptToken(term);
                    break;
                }
            }
        let next = input.next, low = 0, high = data[state + 2];
        // Special case for EOF
        if (input.next < 0 && high > low && data[accEnd + high * 3 - 3] == 65535 /* Seq.End */) {
            state = data[accEnd + high * 3 - 1];
            continue scan;
        }
        // Do a binary search on the state's edges
        for (; low < high;) {
            let mid = (low + high) >> 1;
            let index = accEnd + mid + (mid << 1);
            let from = data[index], to = data[index + 1] || 0x10000;
            if (next < from)
                high = mid;
            else if (next >= to)
                low = mid + 1;
            else {
                state = data[index + 2];
                input.advance();
                continue scan;
            }
        }
        break;
    }
}
function findOffset(data, start, term) {
    for (let i = start, next; (next = data[i]) != 65535 /* Seq.End */; i++)
        if (next == term)
            return i - start;
    return -1;
}
function overrides(token, prev, tableData, tableOffset) {
    let iPrev = findOffset(tableData, tableOffset, prev);
    return iPrev < 0 || findOffset(tableData, tableOffset, token) < iPrev;
}

// Environment variable used to control console output
const verbose = typeof process != "undefined" && process.env && /\bparse\b/.test(process.env.LOG);
let stackIDs = null;
function cutAt(tree, pos, side) {
    let cursor = tree.cursor(_lezer_common__WEBPACK_IMPORTED_MODULE_0__.IterMode.IncludeAnonymous);
    cursor.moveTo(pos);
    for (;;) {
        if (!(side < 0 ? cursor.childBefore(pos) : cursor.childAfter(pos)))
            for (;;) {
                if ((side < 0 ? cursor.to < pos : cursor.from > pos) && !cursor.type.isError)
                    return side < 0 ? Math.max(0, Math.min(cursor.to - 1, pos - 25 /* Lookahead.Margin */))
                        : Math.min(tree.length, Math.max(cursor.from + 1, pos + 25 /* Lookahead.Margin */));
                if (side < 0 ? cursor.prevSibling() : cursor.nextSibling())
                    break;
                if (!cursor.parent())
                    return side < 0 ? 0 : tree.length;
            }
    }
}
class FragmentCursor {
    constructor(fragments, nodeSet) {
        this.fragments = fragments;
        this.nodeSet = nodeSet;
        this.i = 0;
        this.fragment = null;
        this.safeFrom = -1;
        this.safeTo = -1;
        this.trees = [];
        this.start = [];
        this.index = [];
        this.nextFragment();
    }
    nextFragment() {
        let fr = this.fragment = this.i == this.fragments.length ? null : this.fragments[this.i++];
        if (fr) {
            this.safeFrom = fr.openStart ? cutAt(fr.tree, fr.from + fr.offset, 1) - fr.offset : fr.from;
            this.safeTo = fr.openEnd ? cutAt(fr.tree, fr.to + fr.offset, -1) - fr.offset : fr.to;
            while (this.trees.length) {
                this.trees.pop();
                this.start.pop();
                this.index.pop();
            }
            this.trees.push(fr.tree);
            this.start.push(-fr.offset);
            this.index.push(0);
            this.nextStart = this.safeFrom;
        }
        else {
            this.nextStart = 1e9;
        }
    }
    // `pos` must be >= any previously given `pos` for this cursor
    nodeAt(pos) {
        if (pos < this.nextStart)
            return null;
        while (this.fragment && this.safeTo <= pos)
            this.nextFragment();
        if (!this.fragment)
            return null;
        for (;;) {
            let last = this.trees.length - 1;
            if (last < 0) { // End of tree
                this.nextFragment();
                return null;
            }
            let top = this.trees[last], index = this.index[last];
            if (index == top.children.length) {
                this.trees.pop();
                this.start.pop();
                this.index.pop();
                continue;
            }
            let next = top.children[index];
            let start = this.start[last] + top.positions[index];
            if (start > pos) {
                this.nextStart = start;
                return null;
            }
            if (next instanceof _lezer_common__WEBPACK_IMPORTED_MODULE_0__.Tree) {
                if (start == pos) {
                    if (start < this.safeFrom)
                        return null;
                    let end = start + next.length;
                    if (end <= this.safeTo) {
                        let lookAhead = next.prop(_lezer_common__WEBPACK_IMPORTED_MODULE_0__.NodeProp.lookAhead);
                        if (!lookAhead || end + lookAhead < this.fragment.to)
                            return next;
                    }
                }
                this.index[last]++;
                if (start + next.length >= Math.max(this.safeFrom, pos)) { // Enter this node
                    this.trees.push(next);
                    this.start.push(start);
                    this.index.push(0);
                }
            }
            else {
                this.index[last]++;
                this.nextStart = start + next.length;
            }
        }
    }
}
class TokenCache {
    constructor(parser, stream) {
        this.stream = stream;
        this.tokens = [];
        this.mainToken = null;
        this.actions = [];
        this.tokens = parser.tokenizers.map(_ => new CachedToken);
    }
    getActions(stack) {
        let actionIndex = 0;
        let main = null;
        let { parser } = stack.p, { tokenizers } = parser;
        let mask = parser.stateSlot(stack.state, 3 /* ParseState.TokenizerMask */);
        let context = stack.curContext ? stack.curContext.hash : 0;
        let lookAhead = 0;
        for (let i = 0; i < tokenizers.length; i++) {
            if (((1 << i) & mask) == 0)
                continue;
            let tokenizer = tokenizers[i], token = this.tokens[i];
            if (main && !tokenizer.fallback)
                continue;
            if (tokenizer.contextual || token.start != stack.pos || token.mask != mask || token.context != context) {
                this.updateCachedToken(token, tokenizer, stack);
                token.mask = mask;
                token.context = context;
            }
            if (token.lookAhead > token.end + 25 /* Lookahead.Margin */)
                lookAhead = Math.max(token.lookAhead, lookAhead);
            if (token.value != 0 /* Term.Err */) {
                let startIndex = actionIndex;
                if (token.extended > -1)
                    actionIndex = this.addActions(stack, token.extended, token.end, actionIndex);
                actionIndex = this.addActions(stack, token.value, token.end, actionIndex);
                if (!tokenizer.extend) {
                    main = token;
                    if (actionIndex > startIndex)
                        break;
                }
            }
        }
        while (this.actions.length > actionIndex)
            this.actions.pop();
        if (lookAhead)
            stack.setLookAhead(lookAhead);
        if (!main && stack.pos == this.stream.end) {
            main = new CachedToken;
            main.value = stack.p.parser.eofTerm;
            main.start = main.end = stack.pos;
            actionIndex = this.addActions(stack, main.value, main.end, actionIndex);
        }
        this.mainToken = main;
        return this.actions;
    }
    getMainToken(stack) {
        if (this.mainToken)
            return this.mainToken;
        let main = new CachedToken, { pos, p } = stack;
        main.start = pos;
        main.end = Math.min(pos + 1, p.stream.end);
        main.value = pos == p.stream.end ? p.parser.eofTerm : 0 /* Term.Err */;
        return main;
    }
    updateCachedToken(token, tokenizer, stack) {
        let start = this.stream.clipPos(stack.pos);
        tokenizer.token(this.stream.reset(start, token), stack);
        if (token.value > -1) {
            let { parser } = stack.p;
            for (let i = 0; i < parser.specialized.length; i++)
                if (parser.specialized[i] == token.value) {
                    let result = parser.specializers[i](this.stream.read(token.start, token.end), stack);
                    if (result >= 0 && stack.p.parser.dialect.allows(result >> 1)) {
                        if ((result & 1) == 0 /* Specialize.Specialize */)
                            token.value = result >> 1;
                        else
                            token.extended = result >> 1;
                        break;
                    }
                }
        }
        else {
            token.value = 0 /* Term.Err */;
            token.end = this.stream.clipPos(start + 1);
        }
    }
    putAction(action, token, end, index) {
        // Don't add duplicate actions
        for (let i = 0; i < index; i += 3)
            if (this.actions[i] == action)
                return index;
        this.actions[index++] = action;
        this.actions[index++] = token;
        this.actions[index++] = end;
        return index;
    }
    addActions(stack, token, end, index) {
        let { state } = stack, { parser } = stack.p, { data } = parser;
        for (let set = 0; set < 2; set++) {
            for (let i = parser.stateSlot(state, set ? 2 /* ParseState.Skip */ : 1 /* ParseState.Actions */);; i += 3) {
                if (data[i] == 65535 /* Seq.End */) {
                    if (data[i + 1] == 1 /* Seq.Next */) {
                        i = pair(data, i + 2);
                    }
                    else {
                        if (index == 0 && data[i + 1] == 2 /* Seq.Other */)
                            index = this.putAction(pair(data, i + 2), token, end, index);
                        break;
                    }
                }
                if (data[i] == token)
                    index = this.putAction(pair(data, i + 1), token, end, index);
            }
        }
        return index;
    }
}
class Parse {
    constructor(parser, input, fragments, ranges) {
        this.parser = parser;
        this.input = input;
        this.ranges = ranges;
        this.recovering = 0;
        this.nextStackID = 0x2654; // ♔, ♕, ♖, ♗, ♘, ♙, ♠, ♡, ♢, ♣, ♤, ♥, ♦, ♧
        this.minStackPos = 0;
        this.reused = [];
        this.stoppedAt = null;
        this.lastBigReductionStart = -1;
        this.lastBigReductionSize = 0;
        this.bigReductionCount = 0;
        this.stream = new InputStream(input, ranges);
        this.tokens = new TokenCache(parser, this.stream);
        this.topTerm = parser.top[1];
        let { from } = ranges[0];
        this.stacks = [Stack.start(this, parser.top[0], from)];
        this.fragments = fragments.length && this.stream.end - from > parser.bufferLength * 4
            ? new FragmentCursor(fragments, parser.nodeSet) : null;
    }
    get parsedPos() {
        return this.minStackPos;
    }
    // Move the parser forward. This will process all parse stacks at
    // `this.pos` and try to advance them to a further position. If no
    // stack for such a position is found, it'll start error-recovery.
    //
    // When the parse is finished, this will return a syntax tree. When
    // not, it returns `null`.
    advance() {
        let stacks = this.stacks, pos = this.minStackPos;
        // This will hold stacks beyond `pos`.
        let newStacks = this.stacks = [];
        let stopped, stoppedTokens;
        // If a large amount of reductions happened with the same start
        // position, force the stack out of that production in order to
        // avoid creating a tree too deep to recurse through.
        // (This is an ugly kludge, because unfortunately there is no
        // straightforward, cheap way to check for this happening, due to
        // the history of reductions only being available in an
        // expensive-to-access format in the stack buffers.)
        if (this.bigReductionCount > 300 /* Rec.MaxLeftAssociativeReductionCount */ && stacks.length == 1) {
            let [s] = stacks;
            while (s.forceReduce() && s.stack.length && s.stack[s.stack.length - 2] >= this.lastBigReductionStart) { }
            this.bigReductionCount = this.lastBigReductionSize = 0;
        }
        // Keep advancing any stacks at `pos` until they either move
        // forward or can't be advanced. Gather stacks that can't be
        // advanced further in `stopped`.
        for (let i = 0; i < stacks.length; i++) {
            let stack = stacks[i];
            for (;;) {
                this.tokens.mainToken = null;
                if (stack.pos > pos) {
                    newStacks.push(stack);
                }
                else if (this.advanceStack(stack, newStacks, stacks)) {
                    continue;
                }
                else {
                    if (!stopped) {
                        stopped = [];
                        stoppedTokens = [];
                    }
                    stopped.push(stack);
                    let tok = this.tokens.getMainToken(stack);
                    stoppedTokens.push(tok.value, tok.end);
                }
                break;
            }
        }
        if (!newStacks.length) {
            let finished = stopped && findFinished(stopped);
            if (finished) {
                if (verbose)
                    console.log("Finish with " + this.stackID(finished));
                return this.stackToTree(finished);
            }
            if (this.parser.strict) {
                if (verbose && stopped)
                    console.log("Stuck with token " + (this.tokens.mainToken ? this.parser.getName(this.tokens.mainToken.value) : "none"));
                throw new SyntaxError("No parse at " + pos);
            }
            if (!this.recovering)
                this.recovering = 5 /* Rec.Distance */;
        }
        if (this.recovering && stopped) {
            let finished = this.stoppedAt != null && stopped[0].pos > this.stoppedAt ? stopped[0]
                : this.runRecovery(stopped, stoppedTokens, newStacks);
            if (finished) {
                if (verbose)
                    console.log("Force-finish " + this.stackID(finished));
                return this.stackToTree(finished.forceAll());
            }
        }
        if (this.recovering) {
            let maxRemaining = this.recovering == 1 ? 1 : this.recovering * 3 /* Rec.MaxRemainingPerStep */;
            if (newStacks.length > maxRemaining) {
                newStacks.sort((a, b) => b.score - a.score);
                while (newStacks.length > maxRemaining)
                    newStacks.pop();
            }
            if (newStacks.some(s => s.reducePos > pos))
                this.recovering--;
        }
        else if (newStacks.length > 1) {
            // Prune stacks that are in the same state, or that have been
            // running without splitting for a while, to avoid getting stuck
            // with multiple successful stacks running endlessly on.
            outer: for (let i = 0; i < newStacks.length - 1; i++) {
                let stack = newStacks[i];
                for (let j = i + 1; j < newStacks.length; j++) {
                    let other = newStacks[j];
                    if (stack.sameState(other) ||
                        stack.buffer.length > 500 /* Rec.MinBufferLengthPrune */ && other.buffer.length > 500 /* Rec.MinBufferLengthPrune */) {
                        if (((stack.score - other.score) || (stack.buffer.length - other.buffer.length)) > 0) {
                            newStacks.splice(j--, 1);
                        }
                        else {
                            newStacks.splice(i--, 1);
                            continue outer;
                        }
                    }
                }
            }
            if (newStacks.length > 12 /* Rec.MaxStackCount */) {
                newStacks.sort((a, b) => b.score - a.score);
                newStacks.splice(12 /* Rec.MaxStackCount */, newStacks.length - 12 /* Rec.MaxStackCount */);
            }
        }
        this.minStackPos = newStacks[0].pos;
        for (let i = 1; i < newStacks.length; i++)
            if (newStacks[i].pos < this.minStackPos)
                this.minStackPos = newStacks[i].pos;
        return null;
    }
    stopAt(pos) {
        if (this.stoppedAt != null && this.stoppedAt < pos)
            throw new RangeError("Can't move stoppedAt forward");
        this.stoppedAt = pos;
    }
    // Returns an updated version of the given stack, or null if the
    // stack can't advance normally. When `split` and `stacks` are
    // given, stacks split off by ambiguous operations will be pushed to
    // `split`, or added to `stacks` if they move `pos` forward.
    advanceStack(stack, stacks, split) {
        let start = stack.pos, { parser } = this;
        let base = verbose ? this.stackID(stack) + " -> " : "";
        if (this.stoppedAt != null && start > this.stoppedAt)
            return stack.forceReduce() ? stack : null;
        if (this.fragments) {
            let strictCx = stack.curContext && stack.curContext.tracker.strict, cxHash = strictCx ? stack.curContext.hash : 0;
            for (let cached = this.fragments.nodeAt(start); cached;) {
                let match = this.parser.nodeSet.types[cached.type.id] == cached.type ? parser.getGoto(stack.state, cached.type.id) : -1;
                if (match > -1 && cached.length && (!strictCx || (cached.prop(_lezer_common__WEBPACK_IMPORTED_MODULE_0__.NodeProp.contextHash) || 0) == cxHash)) {
                    stack.useNode(cached, match);
                    if (verbose)
                        console.log(base + this.stackID(stack) + ` (via reuse of ${parser.getName(cached.type.id)})`);
                    return true;
                }
                if (!(cached instanceof _lezer_common__WEBPACK_IMPORTED_MODULE_0__.Tree) || cached.children.length == 0 || cached.positions[0] > 0)
                    break;
                let inner = cached.children[0];
                if (inner instanceof _lezer_common__WEBPACK_IMPORTED_MODULE_0__.Tree && cached.positions[0] == 0)
                    cached = inner;
                else
                    break;
            }
        }
        let defaultReduce = parser.stateSlot(stack.state, 4 /* ParseState.DefaultReduce */);
        if (defaultReduce > 0) {
            stack.reduce(defaultReduce);
            if (verbose)
                console.log(base + this.stackID(stack) + ` (via always-reduce ${parser.getName(defaultReduce & 65535 /* Action.ValueMask */)})`);
            return true;
        }
        if (stack.stack.length >= 8400 /* Rec.CutDepth */) {
            while (stack.stack.length > 6000 /* Rec.CutTo */ && stack.forceReduce()) { }
        }
        let actions = this.tokens.getActions(stack);
        for (let i = 0; i < actions.length;) {
            let action = actions[i++], term = actions[i++], end = actions[i++];
            let last = i == actions.length || !split;
            let localStack = last ? stack : stack.split();
            let main = this.tokens.mainToken;
            localStack.apply(action, term, main ? main.start : localStack.pos, end);
            if (verbose)
                console.log(base + this.stackID(localStack) + ` (via ${(action & 65536 /* Action.ReduceFlag */) == 0 ? "shift"
                    : `reduce of ${parser.getName(action & 65535 /* Action.ValueMask */)}`} for ${parser.getName(term)} @ ${start}${localStack == stack ? "" : ", split"})`);
            if (last)
                return true;
            else if (localStack.pos > start)
                stacks.push(localStack);
            else
                split.push(localStack);
        }
        return false;
    }
    // Advance a given stack forward as far as it will go. Returns the
    // (possibly updated) stack if it got stuck, or null if it moved
    // forward and was given to `pushStackDedup`.
    advanceFully(stack, newStacks) {
        let pos = stack.pos;
        for (;;) {
            if (!this.advanceStack(stack, null, null))
                return false;
            if (stack.pos > pos) {
                pushStackDedup(stack, newStacks);
                return true;
            }
        }
    }
    runRecovery(stacks, tokens, newStacks) {
        let finished = null, restarted = false;
        for (let i = 0; i < stacks.length; i++) {
            let stack = stacks[i], token = tokens[i << 1], tokenEnd = tokens[(i << 1) + 1];
            let base = verbose ? this.stackID(stack) + " -> " : "";
            if (stack.deadEnd) {
                if (restarted)
                    continue;
                restarted = true;
                stack.restart();
                if (verbose)
                    console.log(base + this.stackID(stack) + " (restarted)");
                let done = this.advanceFully(stack, newStacks);
                if (done)
                    continue;
            }
            let force = stack.split(), forceBase = base;
            for (let j = 0; j < 10 /* Rec.ForceReduceLimit */ && force.forceReduce(); j++) {
                if (verbose)
                    console.log(forceBase + this.stackID(force) + " (via force-reduce)");
                let done = this.advanceFully(force, newStacks);
                if (done)
                    break;
                if (verbose)
                    forceBase = this.stackID(force) + " -> ";
            }
            for (let insert of stack.recoverByInsert(token)) {
                if (verbose)
                    console.log(base + this.stackID(insert) + " (via recover-insert)");
                this.advanceFully(insert, newStacks);
            }
            if (this.stream.end > stack.pos) {
                if (tokenEnd == stack.pos) {
                    tokenEnd++;
                    token = 0 /* Term.Err */;
                }
                stack.recoverByDelete(token, tokenEnd);
                if (verbose)
                    console.log(base + this.stackID(stack) + ` (via recover-delete ${this.parser.getName(token)})`);
                pushStackDedup(stack, newStacks);
            }
            else if (!finished || finished.score < force.score) {
                finished = force;
            }
        }
        return finished;
    }
    // Convert the stack's buffer to a syntax tree.
    stackToTree(stack) {
        stack.close();
        return _lezer_common__WEBPACK_IMPORTED_MODULE_0__.Tree.build({ buffer: StackBufferCursor.create(stack),
            nodeSet: this.parser.nodeSet,
            topID: this.topTerm,
            maxBufferLength: this.parser.bufferLength,
            reused: this.reused,
            start: this.ranges[0].from,
            length: stack.pos - this.ranges[0].from,
            minRepeatType: this.parser.minRepeatTerm });
    }
    stackID(stack) {
        let id = (stackIDs || (stackIDs = new WeakMap)).get(stack);
        if (!id)
            stackIDs.set(stack, id = String.fromCodePoint(this.nextStackID++));
        return id + stack;
    }
}
function pushStackDedup(stack, newStacks) {
    for (let i = 0; i < newStacks.length; i++) {
        let other = newStacks[i];
        if (other.pos == stack.pos && other.sameState(stack)) {
            if (newStacks[i].score < stack.score)
                newStacks[i] = stack;
            return;
        }
    }
    newStacks.push(stack);
}
class Dialect {
    constructor(source, flags, disabled) {
        this.source = source;
        this.flags = flags;
        this.disabled = disabled;
    }
    allows(term) { return !this.disabled || this.disabled[term] == 0; }
}
const id = x => x;
/**
Context trackers are used to track stateful context (such as
indentation in the Python grammar, or parent elements in the XML
grammar) needed by external tokenizers. You declare them in a
grammar file as `@context exportName from "module"`.

Context values should be immutable, and can be updated (replaced)
on shift or reduce actions.

The export used in a `@context` declaration should be of this
type.
*/
class ContextTracker {
    /**
    Define a context tracker.
    */
    constructor(spec) {
        this.start = spec.start;
        this.shift = spec.shift || id;
        this.reduce = spec.reduce || id;
        this.reuse = spec.reuse || id;
        this.hash = spec.hash || (() => 0);
        this.strict = spec.strict !== false;
    }
}
/**
Holds the parse tables for a given grammar, as generated by
`lezer-generator`, and provides [methods](#common.Parser) to parse
content with.
*/
class LRParser extends _lezer_common__WEBPACK_IMPORTED_MODULE_0__.Parser {
    /**
    @internal
    */
    constructor(spec) {
        super();
        /**
        @internal
        */
        this.wrappers = [];
        if (spec.version != 14 /* File.Version */)
            throw new RangeError(`Parser version (${spec.version}) doesn't match runtime version (${14 /* File.Version */})`);
        let nodeNames = spec.nodeNames.split(" ");
        this.minRepeatTerm = nodeNames.length;
        for (let i = 0; i < spec.repeatNodeCount; i++)
            nodeNames.push("");
        let topTerms = Object.keys(spec.topRules).map(r => spec.topRules[r][1]);
        let nodeProps = [];
        for (let i = 0; i < nodeNames.length; i++)
            nodeProps.push([]);
        function setProp(nodeID, prop, value) {
            nodeProps[nodeID].push([prop, prop.deserialize(String(value))]);
        }
        if (spec.nodeProps)
            for (let propSpec of spec.nodeProps) {
                let prop = propSpec[0];
                if (typeof prop == "string")
                    prop = _lezer_common__WEBPACK_IMPORTED_MODULE_0__.NodeProp[prop];
                for (let i = 1; i < propSpec.length;) {
                    let next = propSpec[i++];
                    if (next >= 0) {
                        setProp(next, prop, propSpec[i++]);
                    }
                    else {
                        let value = propSpec[i + -next];
                        for (let j = -next; j > 0; j--)
                            setProp(propSpec[i++], prop, value);
                        i++;
                    }
                }
            }
        this.nodeSet = new _lezer_common__WEBPACK_IMPORTED_MODULE_0__.NodeSet(nodeNames.map((name, i) => _lezer_common__WEBPACK_IMPORTED_MODULE_0__.NodeType.define({
            name: i >= this.minRepeatTerm ? undefined : name,
            id: i,
            props: nodeProps[i],
            top: topTerms.indexOf(i) > -1,
            error: i == 0,
            skipped: spec.skippedNodes && spec.skippedNodes.indexOf(i) > -1
        })));
        if (spec.propSources)
            this.nodeSet = this.nodeSet.extend(...spec.propSources);
        this.strict = false;
        this.bufferLength = _lezer_common__WEBPACK_IMPORTED_MODULE_0__.DefaultBufferLength;
        let tokenArray = decodeArray(spec.tokenData);
        this.context = spec.context;
        this.specializerSpecs = spec.specialized || [];
        this.specialized = new Uint16Array(this.specializerSpecs.length);
        for (let i = 0; i < this.specializerSpecs.length; i++)
            this.specialized[i] = this.specializerSpecs[i].term;
        this.specializers = this.specializerSpecs.map(getSpecializer);
        this.states = decodeArray(spec.states, Uint32Array);
        this.data = decodeArray(spec.stateData);
        this.goto = decodeArray(spec.goto);
        this.maxTerm = spec.maxTerm;
        this.tokenizers = spec.tokenizers.map(value => typeof value == "number" ? new TokenGroup(tokenArray, value) : value);
        this.topRules = spec.topRules;
        this.dialects = spec.dialects || {};
        this.dynamicPrecedences = spec.dynamicPrecedences || null;
        this.tokenPrecTable = spec.tokenPrec;
        this.termNames = spec.termNames || null;
        this.maxNode = this.nodeSet.types.length - 1;
        this.dialect = this.parseDialect();
        this.top = this.topRules[Object.keys(this.topRules)[0]];
    }
    createParse(input, fragments, ranges) {
        let parse = new Parse(this, input, fragments, ranges);
        for (let w of this.wrappers)
            parse = w(parse, input, fragments, ranges);
        return parse;
    }
    /**
    Get a goto table entry @internal
    */
    getGoto(state, term, loose = false) {
        let table = this.goto;
        if (term >= table[0])
            return -1;
        for (let pos = table[term + 1];;) {
            let groupTag = table[pos++], last = groupTag & 1;
            let target = table[pos++];
            if (last && loose)
                return target;
            for (let end = pos + (groupTag >> 1); pos < end; pos++)
                if (table[pos] == state)
                    return target;
            if (last)
                return -1;
        }
    }
    /**
    Check if this state has an action for a given terminal @internal
    */
    hasAction(state, terminal) {
        let data = this.data;
        for (let set = 0; set < 2; set++) {
            for (let i = this.stateSlot(state, set ? 2 /* ParseState.Skip */ : 1 /* ParseState.Actions */), next;; i += 3) {
                if ((next = data[i]) == 65535 /* Seq.End */) {
                    if (data[i + 1] == 1 /* Seq.Next */)
                        next = data[i = pair(data, i + 2)];
                    else if (data[i + 1] == 2 /* Seq.Other */)
                        return pair(data, i + 2);
                    else
                        break;
                }
                if (next == terminal || next == 0 /* Term.Err */)
                    return pair(data, i + 1);
            }
        }
        return 0;
    }
    /**
    @internal
    */
    stateSlot(state, slot) {
        return this.states[(state * 6 /* ParseState.Size */) + slot];
    }
    /**
    @internal
    */
    stateFlag(state, flag) {
        return (this.stateSlot(state, 0 /* ParseState.Flags */) & flag) > 0;
    }
    /**
    @internal
    */
    validAction(state, action) {
        return !!this.allActions(state, a => a == action ? true : null);
    }
    /**
    @internal
    */
    allActions(state, action) {
        let deflt = this.stateSlot(state, 4 /* ParseState.DefaultReduce */);
        let result = deflt ? action(deflt) : undefined;
        for (let i = this.stateSlot(state, 1 /* ParseState.Actions */); result == null; i += 3) {
            if (this.data[i] == 65535 /* Seq.End */) {
                if (this.data[i + 1] == 1 /* Seq.Next */)
                    i = pair(this.data, i + 2);
                else
                    break;
            }
            result = action(pair(this.data, i + 1));
        }
        return result;
    }
    /**
    Get the states that can follow this one through shift actions or
    goto jumps. @internal
    */
    nextStates(state) {
        let result = [];
        for (let i = this.stateSlot(state, 1 /* ParseState.Actions */);; i += 3) {
            if (this.data[i] == 65535 /* Seq.End */) {
                if (this.data[i + 1] == 1 /* Seq.Next */)
                    i = pair(this.data, i + 2);
                else
                    break;
            }
            if ((this.data[i + 2] & (65536 /* Action.ReduceFlag */ >> 16)) == 0) {
                let value = this.data[i + 1];
                if (!result.some((v, i) => (i & 1) && v == value))
                    result.push(this.data[i], value);
            }
        }
        return result;
    }
    /**
    Configure the parser. Returns a new parser instance that has the
    given settings modified. Settings not provided in `config` are
    kept from the original parser.
    */
    configure(config) {
        // Hideous reflection-based kludge to make it easy to create a
        // slightly modified copy of a parser.
        let copy = Object.assign(Object.create(LRParser.prototype), this);
        if (config.props)
            copy.nodeSet = this.nodeSet.extend(...config.props);
        if (config.top) {
            let info = this.topRules[config.top];
            if (!info)
                throw new RangeError(`Invalid top rule name ${config.top}`);
            copy.top = info;
        }
        if (config.tokenizers)
            copy.tokenizers = this.tokenizers.map(t => {
                let found = config.tokenizers.find(r => r.from == t);
                return found ? found.to : t;
            });
        if (config.specializers) {
            copy.specializers = this.specializers.slice();
            copy.specializerSpecs = this.specializerSpecs.map((s, i) => {
                let found = config.specializers.find(r => r.from == s.external);
                if (!found)
                    return s;
                let spec = Object.assign(Object.assign({}, s), { external: found.to });
                copy.specializers[i] = getSpecializer(spec);
                return spec;
            });
        }
        if (config.contextTracker)
            copy.context = config.contextTracker;
        if (config.dialect)
            copy.dialect = this.parseDialect(config.dialect);
        if (config.strict != null)
            copy.strict = config.strict;
        if (config.wrap)
            copy.wrappers = copy.wrappers.concat(config.wrap);
        if (config.bufferLength != null)
            copy.bufferLength = config.bufferLength;
        return copy;
    }
    /**
    Tells you whether any [parse wrappers](#lr.ParserConfig.wrap)
    are registered for this parser.
    */
    hasWrappers() {
        return this.wrappers.length > 0;
    }
    /**
    Returns the name associated with a given term. This will only
    work for all terms when the parser was generated with the
    `--names` option. By default, only the names of tagged terms are
    stored.
    */
    getName(term) {
        return this.termNames ? this.termNames[term] : String(term <= this.maxNode && this.nodeSet.types[term].name || term);
    }
    /**
    The eof term id is always allocated directly after the node
    types. @internal
    */
    get eofTerm() { return this.maxNode + 1; }
    /**
    The type of top node produced by the parser.
    */
    get topNode() { return this.nodeSet.types[this.top[1]]; }
    /**
    @internal
    */
    dynamicPrecedence(term) {
        let prec = this.dynamicPrecedences;
        return prec == null ? 0 : prec[term] || 0;
    }
    /**
    @internal
    */
    parseDialect(dialect) {
        let values = Object.keys(this.dialects), flags = values.map(() => false);
        if (dialect)
            for (let part of dialect.split(" ")) {
                let id = values.indexOf(part);
                if (id >= 0)
                    flags[id] = true;
            }
        let disabled = null;
        for (let i = 0; i < values.length; i++)
            if (!flags[i]) {
                for (let j = this.dialects[values[i]], id; (id = this.data[j++]) != 65535 /* Seq.End */;)
                    (disabled || (disabled = new Uint8Array(this.maxTerm + 1)))[id] = 1;
            }
        return new Dialect(dialect, flags, disabled);
    }
    /**
    Used by the output of the parser generator. Not available to
    user code. @hide
    */
    static deserialize(spec) {
        return new LRParser(spec);
    }
}
function pair(data, off) { return data[off] | (data[off + 1] << 16); }
function findFinished(stacks) {
    let best = null;
    for (let stack of stacks) {
        let stopped = stack.p.stoppedAt;
        if ((stack.pos == stack.p.stream.end || stopped != null && stack.pos > stopped) &&
            stack.p.parser.stateFlag(stack.state, 2 /* StateFlag.Accepting */) &&
            (!best || best.score < stack.score))
            best = stack;
    }
    return best;
}
function getSpecializer(spec) {
    if (spec.external) {
        let mask = spec.extend ? 1 /* Specialize.Extend */ : 0 /* Specialize.Specialize */;
        return (value, stack) => (spec.external(value, stack) << 1) | mask;
    }
    return spec.get;
}




/***/ },

/***/ "../../frontend/js/features/source-editor/lezer-bibtex/bibtex.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   parser: () => (/* binding */ parser)
/* harmony export */ });
/* harmony import */ var _lezer_lr__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("../../.yarn/cache/@lezer-lr-npm-1.4.7-c15665133d-543c2e1aae.zip/node_modules/@lezer/lr/dist/index.js");
/* harmony import */ var _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__("../../frontend/js/features/source-editor/lezer-bibtex/tokens.mjs");
/* harmony import */ var _highlight_mjs__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__("../../frontend/js/features/source-editor/lezer-bibtex/highlight.mjs");
// This file was generated by lezer-generator. You probably shouldn't edit it.



const parser = _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.LRParser.deserialize({
  version: 14,
  states: "/SQYQPOOObQQO'#CaOpQQO'#C`OxQQO'#CwO!QQQO'#DQO!YQQO'#DXOOQO'#D`'#D`QYQPOOOOQO,58{,58{OOQO,59d,59dOOQO,59m,59mOOQO,59t,59tOOQO'#Cd'#CdOOQO'#Cu'#CuO!bQSO'#DmO!bQSO'#DmOOQO'#Cc'#CcOOQO,58z,58zOOOO'#Cz'#CzOOOO'#C}'#C}O!gO`O'#CyO#^O`O'#CyOOQO,59c,59cOOQO'#DT'#DTOOQO'#DV'#DVO#eQWO'#ERO#eQWO'#EROOQO'#DS'#DSOOQO,59l,59lOOQO'#D['#D[OOQO'#D^'#D^O#sQWO'#ESO#{QWO'#ESOOQO'#DZ'#DZOOQO,59s,59sOOQO-E7^-E7^O$TQQO'#DoO$`QQO,5:XO$eQQO,5:XO!gO`O'#DbOOOO'#Db'#DbO${O`O'#DwOOOO'#C{'#C{O%SO`O,59eO!gO`O'#DeOOOO'#De'#DeO%XO`O'#EQOOOO'#DO'#DOO%`O`O,59eOOOO'#Ck'#CkOOOO'#Cn'#CnO%vO`O'#CjO!gO`O'#CjOOQO'#Cr'#CrOOQO'#Ds'#DsO%}QQO'#CiO&]QQO,5:mO&bQQO,5:mOOQO'#Cg'#CgO&gQQO'#CfO&lQQO'#DqO&wQQO,5:nO&|QQO,5:nO'RQWO,5:ZOOQO'#Ct'#CtOOQO1G/s1G/sOOQO'#Cv'#CvO'^O`O,59|OOOO-E7`-E7`OOQO'#C|'#C|OOQO1G/P1G/PO'cO`O,5:POOOO-E7c-E7cOOQO'#DP'#DPO!gO`O'#DaOOOO'#Da'#DaO'hO`O'#DuOOOO'#Cl'#ClO'oO`O,59UOOOO'#Co'#CoO'tO`O,59UO#eQWO'#DcO'yQQO,59TOOQO'#DU'#DUOOQO1G0X1G0XOOQO'#DW'#DWO#eQWO,59QO(XQWO,5:]O(dQQO,5:]OOQO'#D]'#D]OOQO1G0Y1G0YOOQO'#D_'#D_OOQO1G/u1G/uOOOO1G/h1G/hOOOO1G/k1G/kO(oO`O,59{OOOO-E7_-E7_OOQO'#Cm'#CmOOQO1G.p1G.pOOQO'#Cp'#CpOOQO,59},59}OOQO-E7a-E7aOOQO1G.l1G.lOOQO,5:O,5:OO(tQWO1G/wOOQO-E7b-E7bOOOO1G/g1G/gP)PQWO'#Dd",
  stateData: ")U~O!_OSPOS~ORUO!`PO~OUWO!ZZO![YO!]XO~O!b[O!r]O~O!bbO!rcO~O!bgO!rhO~O!bmO!rnO~OXtO~O!jwO!mxO!nxO!oxO!pxO!l!kP~O!j|O!l}O!m}O!n}O!p}O~O!o!tP~P!{Oe!WO!b!SO!f!VO!h!RO~O!f![O!q!eP~O!f![O!s!eP~O!d!aO!q!cX!s!cX~O!q!bO~O!s!dO~O!jwO!mxO!nxO!oxO!pxO~O!l!kX~P$jO!l!gO~O!o!tX~P!{O!o!kO~O!j!lO!l!mO!n!mO!o!mO!p!mO~O!m!iP~P%eOg!sO!q]X!s]X!d]X~O!q!uO~O!s!wO~O[!xO~O!d!yO!q!eX!s!eX~O!q!{O~O!s!}O~O!f![O!q!eP!s!eP~O!l#PO~O!l#QO~O!m!iX~P%eO!m#TO~O!l#VO~Og!sO!q]a!s]a!d]a~O!f![O!q!ea!s!ea~O!d#[O!q!ea!s!ea~O!l#^O~O!f![O!q!ei!s!ei~O!f![O~O",
  goto: "(O!wPPPP!x!|P#Q#TP#W#bP#j#s#y$P$S$V$]$`P#sP$c$f$i!x$l$p$s$v$y$|%P%S!x%V%Z%^%a%d%g!x%j%n%q%t%w%z%}&T&Z&e&k&qPPPPPPP&wP&zP'QP'ZP'cP'fPPPPPPPP'u'x'{TUOVTQOVRaQR^QU!^op!aV#Z!y#[#_]!]op!a!y#[#_Q!YiQ!ZjR#Y!xX!Wij!s!xX!Tij!s!xR!p!TR#U!pX!Uij!s!xR!r!UR#U!rR!cuR_QR!cvTROVRfRRdRR{dR!h{ReRR!QeR!h!QTSOVRlSRiSR!v!YRjSR!v!ZTTOVRrTRoTR!|!_RpTR!|!`QVORsVQ!n!TR#S!nYydw|!U!lR!fyQ!t!XR#X!tQ!z!^R#]!zQ!OeR!j!OR`QQu^Rv_Q!_oQ!`pR#O!aU!Xij!xR#W!sR!o!TQzdQ!ewQ!i|Q!q!UR#R!lR!PeRkSRqT",
  nodeNames: "⚠ Comment Bibliography Junk Entry EntryCommand EntryType EntryBody BodyOpen CitationKey Field FieldName = Value StringLiteral StringOpen StringContents StringClose StringOpen StringContents StringClose NumberLiteral StringName # BodyClose BodyOpen BodyClose CommentEntry CommentCommand CommentBody BodyOpen CommentContents BodyClose BodyOpen CommentContents BodyClose PreambleEntry PreambleCommand PreambleBody BodyOpen BodyClose BodyOpen BodyClose StringEntry StringCommand StringBody BodyOpen BodyClose BodyOpen BodyClose",
  maxTerm: 84,
  nodeProps: [
    ["closedBy", -8,8,25,30,33,39,41,46,48,"BodyClose",-2,15,18,"StringClose"],
    ["openedBy", -2,17,20,"StringOpen",-8,24,26,32,35,40,42,47,49,"BodyOpen"]
  ],
  propSources: [_highlight_mjs__WEBPACK_IMPORTED_MODULE_2__.highlighting],
  skippedNodes: [0,1],
  repeatNodeCount: 6,
  tokenData: "3`~RrOX#]XY%SYZ%SZ]#]]^%S^p#]pq%Sqr%ers'kst(etu%euv)_vw%ewx#]xy)yyz*sz|%e|}+m}!Q%e!Q![,g![!_%e!_!`0m!`!b%e!b!c1g!c#O%e#O#P#]#P#o%e#o#p1l#p#q%e#q#r2f#r#t#]#t;'S%e;'S;=`'e<%lO%eQ#bZRQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tq!b#]!c;'S#];'S;=`$|<%lO#]Q$W[OX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tqu#]v!b#]!c;'S#];'S;=`$|<%lO#]Q%PP;=`<%l#]~%XS!_~XY%SYZ%S]^%Spq%Sn%pkXWUSRQ!f`OX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tqr%ert#]tu%euv#]vw%ewz#]z|%e|}#]}!_%e!_!`#]!`!b%e!c#O%e#O#P#]#P#o%e#o#p#]#p#q%e#q#t#]#t;'S%e;'S;=`'e<%lO%en'hP;=`<%l%eb'rZ!h`RQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tq!b#]!c;'S#];'S;=`$|<%lO#]U(lZgSRQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tq!b#]!c;'S#];'S;=`$|<%lO#]~)dTP~OY)_Z])_^;'S)_;'S;=`)s<%lO)_~)vP;=`<%l)_U*QZ!rSRQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tq!b#]!c;'S#];'S;=`$|<%lO#]f*zZ!sdRQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tq!b#]!c;'S#];'S;=`$|<%lO#]U+tZ!dSRQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tq!b#]!c;'S#];'S;=`$|<%lO#]j,pmXWRQe`OX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tqr.krt#]tu.kuv#]vw.kwz#]z|.k|}#]}!Q.k!Q![,g![!_.k!_!`#]!`!b.k!c#O.k#O#P#]#P#o.k#o#p#]#p#q.k#q#t#]#t;'S.k;'S;=`0g<%lO.kY.rkXWRQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tqr.krt#]tu.kuv#]vw.kwz#]z|.k|}#]}!_.k!_!`#]!`!b.k!c#O.k#O#P#]#P#o.k#o#p#]#p#q.k#q#t#]#t;'S.k;'S;=`0g<%lO.kY0jP;=`<%l.kU0tZ[SRQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tq!b#]!c;'S#];'S;=`$|<%lO#]~1lO!`~f1sZ!bdRQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tq!b#]!c;'S#];'S;=`$|<%lO#]f2mZ!qdRQOX#]XY$TYZ$TZ]#]]^$T^p#]pq$Tq!b#]!c;'S#];'S;=`$|<%lO#]",
  tokenizers: [1, 2, 3, 4, new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.LocalTokenGroup("|~RTrsbxygyzl#o#pq#q#rv~gO!m~~lO!n~~qO!o~~vO!j~~{O!l~~", 43, 78)],
  topRules: {"Bibliography":[0,2]},
  specialized: [{term: 6, get: (value, stack) => ((0,_tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.specializeEntryType)(value, stack) << 1), external: _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.specializeEntryType}],
  tokenPrec: 0
})


/***/ },

/***/ "../../frontend/js/features/source-editor/lezer-bibtex/bibtex.terms.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   Bibliography: () => (/* binding */ Bibliography),
/* harmony export */   CitationKey: () => (/* binding */ CitationKey),
/* harmony export */   Comment: () => (/* binding */ Comment),
/* harmony export */   CommentBody: () => (/* binding */ CommentBody),
/* harmony export */   CommentCommand: () => (/* binding */ CommentCommand),
/* harmony export */   CommentEntry: () => (/* binding */ CommentEntry),
/* harmony export */   Entry: () => (/* binding */ Entry),
/* harmony export */   EntryBody: () => (/* binding */ EntryBody),
/* harmony export */   EntryCommand: () => (/* binding */ EntryCommand),
/* harmony export */   EntryType: () => (/* binding */ EntryType),
/* harmony export */   Field: () => (/* binding */ Field),
/* harmony export */   FieldName: () => (/* binding */ FieldName),
/* harmony export */   Junk: () => (/* binding */ Junk),
/* harmony export */   NumberLiteral: () => (/* binding */ NumberLiteral),
/* harmony export */   PreambleBody: () => (/* binding */ PreambleBody),
/* harmony export */   PreambleCommand: () => (/* binding */ PreambleCommand),
/* harmony export */   PreambleEntry: () => (/* binding */ PreambleEntry),
/* harmony export */   StringBody: () => (/* binding */ StringBody),
/* harmony export */   StringCommand: () => (/* binding */ StringCommand),
/* harmony export */   StringEntry: () => (/* binding */ StringEntry),
/* harmony export */   StringLiteral: () => (/* binding */ StringLiteral),
/* harmony export */   StringName: () => (/* binding */ StringName),
/* harmony export */   Value: () => (/* binding */ Value),
/* harmony export */   commentKeyword: () => (/* binding */ commentKeyword),
/* harmony export */   preambleKeyword: () => (/* binding */ preambleKeyword),
/* harmony export */   stringKeyword: () => (/* binding */ stringKeyword)
/* harmony export */ });
// This file was generated by lezer-generator. You probably shouldn't edit it.
const
  stringKeyword = 57,
  preambleKeyword = 58,
  commentKeyword = 59,
  Comment = 1,
  Bibliography = 2,
  Junk = 3,
  Entry = 4,
  EntryCommand = 5,
  EntryType = 6,
  EntryBody = 7,
  CitationKey = 9,
  Field = 10,
  FieldName = 11,
  Value = 13,
  StringLiteral = 14,
  NumberLiteral = 21,
  StringName = 22,
  CommentEntry = 27,
  CommentCommand = 28,
  CommentBody = 29,
  PreambleEntry = 36,
  PreambleCommand = 37,
  PreambleBody = 38,
  StringEntry = 43,
  StringCommand = 44,
  StringBody = 45


/***/ },

/***/ "../../frontend/js/features/source-editor/lezer-bibtex/highlight.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   highlighting: () => (/* binding */ highlighting)
/* harmony export */ });
/* harmony import */ var _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("../../.yarn/cache/@lezer-highlight-npm-1.2.3-e3bf6a2cc7-3bcb4fce7a.zip/node_modules/@lezer/highlight/dist/index.js");


const highlighting = (0,_lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.styleTags)({
  'EntryCommand/...': _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.keyword,
  'StringCommand/...': _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.keyword,
  'PreambleCommand/...': _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.keyword,
  'CommentCommand/...': _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.keyword,
  FieldName: _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.name,
  CitationKey: _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.name,
  'StringLiteral/...': _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.string,
  NumberLiteral: _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.number,
  StringName: _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.variableName,
  '#': _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.operator,
  Comment: _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.comment,
  'CommentBody/...': _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.comment,
  Junk: _lezer_highlight__WEBPACK_IMPORTED_MODULE_0__.tags.comment,
})


/***/ },

/***/ "../../frontend/js/features/source-editor/lezer-bibtex/tokens.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   specializeEntryType: () => (/* binding */ specializeEntryType)
/* harmony export */ });
/* harmony import */ var _bibtex_terms_mjs__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("../../frontend/js/features/source-editor/lezer-bibtex/bibtex.terms.mjs");


/**
 * @param {string} identifier
 */
function specializeEntryType(identifier) {
  const lowercased = identifier.toLowerCase()
  switch (lowercased) {
    case 'string':
      return _bibtex_terms_mjs__WEBPACK_IMPORTED_MODULE_0__.stringKeyword
    case 'preamble':
      return _bibtex_terms_mjs__WEBPACK_IMPORTED_MODULE_0__.preambleKeyword
    case 'comment':
      return _bibtex_terms_mjs__WEBPACK_IMPORTED_MODULE_0__.commentKeyword
  }
  return -1
}


/***/ },

/***/ "../../frontend/js/features/source-editor/lezer-latex/latex.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   parser: () => (/* binding */ parser)
/* harmony export */ });
/* harmony import */ var _lezer_lr__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("../../.yarn/cache/@lezer-lr-npm-1.4.7-c15665133d-543c2e1aae.zip/node_modules/@lezer/lr/dist/index.js");
/* harmony import */ var _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__("../../frontend/js/features/source-editor/lezer-latex/tokens.mjs");
// This file was generated by lezer-generator. You probably shouldn't edit it.


const parser = _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.LRParser.deserialize({
  version: 14,
  states: "$MYOVQ#tOOOOQO'#K|'#K|O&rQ#tO'#EtOVQ#tO'#EsO&wQ#tO'#EmO'SQ#tO'#EyO'_Q#tO'#EzO'jQ#tO'#E{O'uQ#tO'#E|O(QQ#tO'#FQO(]Q#tO'#FSO(eQ#tO'#FUO(mQ#tO'#FWO(xQ#tO'#FYO)QQ#tO'#FZO)YQ#tO'#F[O)bQ&jO'#F^O)mQ#tO'#F_O)uQ#tO'#F`O)}Q#uO'#FaO*SQ#vO'#FbO*_Q#tO'#FcO*jQ#tO'#FfO*uQ&jO'#FhO+TQ#tO'#FiO+]Q#tO'#FkO+hQ&jO'#FkO+vQ#tO'#FmO,RQ&jO'#FmO1dQ#tO'#F{OOQO'#Fz'#FzO2dQ$fO'#FoO2oQ$fO'#GTO2tQ#tO'#GUO2|Q$fO'#GVO3XQ$fO'#GWO3dQ#tO'#GXO3lQ$fO'#GYO3wQ#tO'#GZO4PQ#tO'#G^O4PQ#tO'#G`OOQO'#Gb'#GbO4UQ#tO'#GcO:TQ#tO'#GdO@SQ#tO'#GeOFRQ#tO'#GfOFZQ&jO'#GkOFfQ#tO'#GlOFqQ#tO'#GmOFqQ#tO'#GnOFqQ#tO'#GoOFqQ#tO'#GpOFqQ#tO'#GqOFqQ#tO'#GrOFqQ#tO'#GsOFqQ#tO'#GtOFqQ#tO'#GuOFqQ#tO'#GvOFqQ#tO'#GwOFvQ#tO'#GxOGOQ#tO'#GyOGZQ#tO'#GzOOQO'#El'#ElOLfQ#tO'#G{OOQO'#G{'#G{OOQO'#Ek'#EkO!$]Q,UO'#G|OOQO'#Iq'#IqO!$dQ,UO'#IpOOQO'#It'#ItO!$kQ,UO'#IsOOQO'#K{'#K{OVQ#tO'#EcO!)uQ#tO'#IxO!)|Q,UO'#I}O!)|Q,UO'#JSO!*TQ$UO'#JWO!*YQ#tO'#J]OVQ#tO'#JdOVQ#tO'#JhOVQ#tO'#JlOOQO'#Eb'#EbOOQO'#Kz'#KzO!/`Q#tO'#JpOOQO'#Ky'#KyOOQO'#K['#K[OOQO'#KZ'#KZO!/gQ&jO'#JrO!/uQ&jO'#JuO!0TQ&jO'#JxO!0cQ&jO'#J{O!0qQ&jO'#KOO!1PQ&jO'#KRO!1_Q&jO'#KUO!1mQ&jO'#KXO!1{Q#tO'#KWOOQO'#MO'#MOO!2rQ#tO'#E`QOQ#tOOO!3PQ#tO'#KTO!3vQ#tO'#KQO!4mQ#tO'#J}O!5dQ#tO'#JzO!6ZQ#tO'#JwO!7QQ#tO'#JtO!7wQ#tO'#JqO!8nQ&jO'#EuO!9cQ#tO,5;`O!?OQ#tO,5;OO!DkQ#tO,5?eO!IqQ,UO,5?jO!NnQ,UO,5?oO#%kQ$UO,5?sO#%vQ#tO,5?xO#*|Q#tO,5@PO#0iQ#tO,5@TO#6UQ#tO,5@XOOQO'#Ew'#EwO#;qQ#tO,5;_O#;vQ#tO'#EoOOQO,5;X,5;XOFqQ#tO,5;XO#EyQ#tO'#EhO#FQQ#tO,5;XOOQO,5;e,5;eO#FYQ#tO,5;eO#FbQ#tO,5;eOOQO,5;f,5;fO#FmQ#tO,5;fO#FuQ#tO,5;fOOQO,5;g,5;gO#GQQ#tO,5;gO#GYQ#tO,5;gO#GeQ#tO'#E}OOQO,5;h,5;hO#LhQ#tO,5;hO#LpQ#tO,5;hOOQO'#FR'#FROOQO,5;l,5;lO#L{Q#tO,5;lO#MQQ#tO,5;lOOQO'#FT'#FTOOQO,5;n,5;nO#L{Q#tO,5;nOOQO'#FV'#FVOOQO,5;p,5;pO#L{Q#tO,5;pOOQO'#FX'#FXOOQO,5;r,5;rO#L{Q#tO,5;rO#MQQ#tO,5;rO#MYQ#tO,5;tO#L{Q#tO,5;tO#MbQ#tO,5;uO#L{Q#tO,5;uO#MjQ#xO'#F]O#L{Q#tO,5;vO#MoQ#tO,5;vO#FQQ#tO,5;xO#MtQ#tO,5;xO#L{Q#tO,5;xOOQO,5;y,5;yO#L{Q#tO,5;yOOQO,5;z,5;zO#MoQ#tO,5;zOOQO,5;{,5;{OOQO,5;|,5;|O#M|Q#vO,5;|O#NRQ#vO,5;|O#NZQ#xO'#FeOOQO'#Fd'#FdOOQO,5;},5;}O4PQ#tO,5;}O#N`Q#tO,5;}OOQO'#Fg'#FgOOQO,5<Q,5<QO4PQ#tO,5<QO#N`Q#tO,5<QOOQO,5<S,5<SOFqQ#tO,5<SO#NhQ#tO,5<SO#FQQ#tO,5<SOOQO'#Fj'#FjOOQO,5<T,5<TO#L{Q#tO,5<TOOQO'#Fl'#FlOOQO,5<V,5<VO#NsQ#tO,5<VO#NsQ#tO,5<VO$ OQ#tO,5<VOOQO'#Fn'#FnOOQO,5<X,5<XO$ ZQ#tO,5<XO$ ZQ#tO,5<XO$ fQ#tO,5<XOOQO,5<g,5<gO$ qQ#tO,5<ZO$!SQ$fO,5<ZO$![Q'[O,5<oOOQO,5<p,5<pOFqQ#tO,5<pO$!gQ#tO,5<qO$!uQ#xO,5<qO$!zQ$fO,5<qO$!gQ#tO,5<rO$#SQ#xO,5<rO$#XQ$fO,5<rO$#aQ#xO,5<sO$#fQ#tO,5<sO$!gQ#tO,5<tO$#kQ#xO,5<tO$#pQ$fO,5<tO$#xQ#|O'#G]OOQO'#G['#G[OOQO,5<u,5<uOOQO'#G_'#G_OOQO,5<x,5<xOOQO'#Ga'#GaOOQO,5<z,5<zO$#}Q#tO,5<}OOQO,5<},5<}OOQO,5=O,5=OOOQO,5=P,5=POOQO'#Gg'#GgO$)|Q#tO,5=QO#L{Q#tO,5=QOOQO,5=V,5=VOFqQ#tO,5=VO$*UQ&jO,5=VOOQO'#Kf'#KfOFfQ#tO,5=WO$*^Q#tO,5=WO#FTQ#tO'#KfOOQO,5=X,5=XOOQO,5=Y,5=YOOQO,5=Z,5=ZOOQO,5=[,5=[OOQO,5=],5=]OOQO,5=^,5=^OOQO,5=_,5=_OOQO,5=`,5=`OOQO,5=a,5=aOOQO,5=b,5=bOOQO,5=c,5=cO$*fQ#tO,5=dO#L{Q#tO,5=dOOQO,5=e,5=eO$*nQ#tO,5=eO$*vQ#tO,5=eOOQO,5=f,5=fO$+RQ#tO,5=fO$+ZQ#tO,5=fOOQO'#Kg'#KgO$+fQ#tO,5=gO$+fQ#tO,5=gO$1_Q,UO'#F{O$1lQ#tO'#HRO$1wQ#tO'#HSO$2SQ#tO'#HTO$2_Q#tO'#HUO$2jQ#tO'#HVO$2uQ#tO'#HWO$3QQ#tO'#HYO$3YQ#tO'#H[O$3bQ#tO'#H^O$3mQ#tO'#H_O$3uQ#tO'#HaO$3}Q#tO'#HbO$4VQ&jO'#HcO$4bQ#tO'#HdO$4jQ#tO'#HeO$4rQ#uO'#HfO$4wQ#vO'#HgO$5SQ#tO'#HhO$5_Q#tO'#HjO$5jQ&jO'#HlO$5xQ#tO'#HmO$6QQ#tO'#HnO$6]Q&jO'#HnO$6kQ#tO'#HoO$6vQ&jO'#HoO$7UQ$fO'#HpO$7aQ$fO'#HqO$7fQ#tO'#HrO$7nQ$fO'#HsO$7yQ$fO'#HtO$8UQ#tO'#HuO$8^Q$fO'#HvO3wQ#tO'#HwO4PQ#tO'#HyO4PQ#tO'#H{OOQO'#H}'#H}O$8iQ,UO'#IOO$=uQ,UO'#IPO$CRQ,UO'#IQO$H_Q#tO'#IRO$HgQ&jO'#IUOFfQ#tO'#IVOFqQ#tO'#IWOFqQ#tO'#IXOFqQ#tO'#IYOFqQ#tO'#IZOFqQ#tO'#I[OFqQ#tO'#I]OFqQ#tO'#I^OFqQ#tO'#I_O$HrQ#tO'#I`O$HrQ#tO'#IaO$HrQ#tO'#IbO$HwQ#tO'#IcO$IPQ#tO'#IdO$I[Q#tO'#IeOOQO'#HQ'#HQO$IgQ!!^O'#L[OOQO'#L['#L[OOQO'#If'#IfOOQO'#HP'#HPO$IoQ,UO'#IgO$LPQ7[O'#IiO$LWQ,UO'#IhOOQO'#Kh'#KhO$L_Q,UO'#HOOOQO'#G}'#G}O$LuQ,UO'#IoO$L|Q#tO,5=hOOQO'#Ir'#IrOOQO,5?[,5?[O$MRQ#tO,5?[OOQO'#Iu'#IuOOQO,5?_,5?_O$MWQ#tO,5?_O$M]Q#tO,5:}O$MbQ#tO'#GjOOQO'#I{'#I{O$MlQ#tO,5?dOOQO'#JQ'#JQO$MqQ#tO,5?iO$MvQ#tO,5?nOOQO'#JZ'#JZO$M{Q#tO,5?rO!*YQ#tO'#JbOOQO'#Kj'#KjO$NQQ#tO'#JaOOQO'#J`'#J`O$N[Q#tO,5?wO$NaQ#tO,5@OO$NfQ#tO,5@SO$NkQ#tO,5@WOOQO,5@[,5@[O$NpQ#tO,5@[OOQO-E>Y-E>YO#;vQ#tO'#GSOOQO,5@^,5@^O$NuQ#tO,5@^O$N}Q#tO,5@^O% YQ&jO,5@^OOQO,5@a,5@aO% hQ#tO,5@aO% pQ#tO,5@aO% {Q&jO,5@aOOQO,5@d,5@dO%!ZQ#tO,5@dO%!cQ#tO,5@dO%!nQ&jO,5@dOOQO,5@g,5@gO%!|Q#tO,5@gO%#UQ#tO,5@gO%#aQ&jO,5@gOOQO,5@j,5@jO%#oQ#tO,5@jO%#wQ#tO,5@jO%$SQ&jO,5@jOOQO,5@m,5@mO%$bQ#tO,5@mO%$jQ#tO,5@mO%$uQ&jO,5@mOOQO,5@p,5@pO%%TQ#tO,5@pO%%]Q#tO,5@pO%%hQ&jO,5@pOOQO,5@s,5@sO%%vQ#tO,5@sO%&OQ#tO,5@sO%&ZQ&jO,5@sOOQO'#Kr'#KrO%&iQ#tO'#KYOOQO,5@r,5@rOOQO-E>X-E>XOOQO'#Kq'#KqO%'`Q#tO'#KVOOQO,5@o,5@oOOQO'#Kp'#KpO%(VQ#tO'#KSOOQO,5@l,5@lOOQO'#Ko'#KoO%(|Q#tO'#KPOOQO,5@i,5@iOOQO'#Kn'#KnO%)sQ#tO'#J|OOQO,5@f,5@fOOQO'#Km'#KmO%*jQ#tO'#JyOOQO,5@c,5@cOOQO'#Kl'#KlO%+aQ#tO'#JvOOQO,5@`,5@`OOQO'#Kk'#KkO%,WQ#tO'#JsOOQO,5@],5@]O%,}Q#tO,5;POOQO,5;a,5;aO%-SQ#tO,5;aO%-XQ#tO,5?fO%-^Q#tO,5?kO%-cQ#tO,5?pO%-hQ#tO,5?tO%-mQ#tO,5?yO%-rQ#tO,5@QO%-wQ#tO,5@UO%-|Q#tO,5@YOOQ`'#K_'#K_O%.RQ#tO1G0zO%.RQ#tO1G0zO%3nQ#tO1G0jO%3nQ#tO1G0jO%9ZQ#tO1G5PO%9ZQ#tO1G5PO%>aQ,UO1G5UO%>aQ,UO1G5UO%C^Q,UO1G5ZO%C^Q,UO1G5ZO%HZQ$UO1G5_O%HZQ$UO1G5_O%HcQ#tO1G5dO%HcQ#tO1G5dO%MiQ#tO1G5kO%MiQ#tO1G5kO&%UQ#tO1G5oO&%UQ#tO1G5oO&*qQ#tO1G5sO&*qQ#tO1G5sO&0^Q#tO'#ExOOQO1G0y1G0yO#;vQ#tO'#ErOOQO'#K^'#K^O&5`Q#tO'#EpO&5pQ#tO,5;ZOOQO1G0s1G0sO&5uQ#tO'#IvOOQO'#K]'#K]O&5|Q#tO'#EjO&6WQ#tO,5;SOFqQ#tO1G0sOOQO1G1P1G1POFqQ#tO1G1PO&6]Q#tO1G1POOQO1G1Q1G1QOFqQ#tO1G1QO&6eQ#tO1G1QOOQO1G1R1G1ROFqQ#tO1G1RO&6mQ#tO1G1RO#GeQ#tO'#FPOOQO'#K`'#K`O&6uQ#tO'#FOO&7SQ#tO,5;iOOQO1G1S1G1SO#L{Q#tO1G1SO&7XQ#tO1G1SOOQO1G1W1G1WO#L{Q#tO1G1WOOQO1G1Y1G1YOOQO1G1[1G1[OOQO1G1^1G1^O#L{Q#tO1G1^OOQO1G1`1G1`OFqQ#tO1G1`O&7aQ#tO1G1`OOQO1G1a1G1aOFqQ#tO1G1aO&7iQ#tO1G1aO&7qQ#tO,5;wOOQO1G1b1G1bO#L{Q#tO1G1bO&7vQ#tO1G1dOFqQ#tO1G1dO#FQQ#tO1G1dO#L{Q#tO1G1dOOQO1G1e1G1eOOQO1G1f1G1fOOQO1G1h1G1hO&=uQ#vO1G1hO&=zQ#tO,5<POOQO1G1i1G1iO4PQ#tO1G1iOOQO1G1l1G1lO4PQ#tO1G1lOOQO1G1n1G1nOFqQ#tO1G1nO#FQQ#tO1G1nOOQO1G1o1G1oOOQO1G1q1G1qO&>PQ#tO1G1qO&>XQ#tO1G1qO&>dQ#tO1G1qO&>dQ#tO1G1qOOQO1G1s1G1sO&>oQ#tO1G1sO&>wQ#tO1G1sO&?SQ#tO1G1sO&?SQ#tO1G1sO&?_Q&jO'#FqO$ }Q#tO'#FrOOQO'#Ka'#KaO&?|Q#tO1G1uO&?|Q#tO1G1uO&@_Q#tO'#FsO&F`Q#tO'#FsO&@_Q#tO'#FsOOQO1G1u1G1uO&FgQ#tO1G1uOOQO1G2Z1G2ZO&FxQ$fO1G2ZO&GQQ'[O1G2ZOOQO1G2[1G2[OOQO'#Kb'#KbOOQO'#Ke'#KeO$!gQ#tO1G2]OOQO1G2]1G2]O&G]Q#tO1G2]O$!gQ#tO1G2]O&GbQ#xO1G2]O$!gQ#tO1G2^OOQO1G2^1G2^O&GgQ#tO1G2^O$!gQ#tO1G2^O&GlQ#xO1G2^O&GqQ#tO1G2_O&GvQ#xO1G2_O$!gQ#tO1G2`O&G{Q#tO1G2`O&HWQ#tO1G2`O$!gQ#tO1G2`O&H]Q#xO1G2`OOQO,5<w,5<wOOQO1G2i1G2iOOQO'#Gh'#GhO&HbQ#tO1G2lO#L{Q#tO1G2lO&HjQ#tO1G2lOOQO1G2q1G2qOFqQ#tO1G2qOOQO-E>d-E>dO&HrQ#tO1G2rOOQO1G2r1G2rOFqQ#tO1G2rOOQO,5AQ,5AQOOQO1G3O1G3OO#L{Q#tO1G3OO&HzQ#tO1G3OOOQO1G3P1G3POFqQ#tO1G3PO&ISQ#tO1G3POOQO1G3Q1G3QOFqQ#tO1G3QO&I[Q#tO1G3QOOQO-E>e-E>eOOQO1G3R1G3ROOQO,5=m,5=mOFqQ#tO,5=mO#FQQ#tO,5=mOOQO,5=n,5=nO&IdQ#tO,5=nO&IlQ#tO,5=nOOQO,5=o,5=oO&IwQ#tO,5=oO&JPQ#tO,5=oOOQO,5=p,5=pO&J[Q#tO,5=pO&JdQ#tO,5=pOOQO,5=q,5=qO&JoQ#tO,5=qO&JwQ#tO,5=qOOQO'#HX'#HXOOQO,5=r,5=rO#L{Q#tO,5=rO#MQQ#tO,5=rOOQO'#HZ'#HZOOQO,5=t,5=tO#L{Q#tO,5=tOOQO'#H]'#H]OOQO,5=v,5=vO#L{Q#tO,5=vOOQO,5=x,5=xO#L{Q#tO,5=xO#MQQ#tO,5=xO&KSQ#tO,5=yO#L{Q#tO,5=yO&K[Q#tO,5={O#L{Q#tO,5={O#L{Q#tO,5=|O#MoQ#tO,5=|O#FQQ#tO,5=}O&KdQ#tO,5=}O#L{Q#tO,5=}OOQO,5>O,5>OO#L{Q#tO,5>OOOQO,5>P,5>PO#MoQ#tO,5>POOQO,5>Q,5>QOOQO,5>R,5>RO&KlQ#vO,5>RO&KqQ#vO,5>ROOQO'#Hi'#HiOOQO,5>S,5>SO4PQ#tO,5>SO#N`Q#tO,5>SOOQO'#Hk'#HkOOQO,5>U,5>UO4PQ#tO,5>UO#N`Q#tO,5>UOOQO,5>W,5>WOFqQ#tO,5>WO&KyQ#tO,5>WO#FQQ#tO,5>WOOQO,5>X,5>XO#L{Q#tO,5>XOOQO,5>Y,5>YO&LUQ#tO,5>YO&LUQ#tO,5>YO&LaQ#tO,5>YOOQO,5>Z,5>ZO&LlQ#tO,5>ZO&LlQ#tO,5>ZO&LwQ#tO,5>ZO&MSQ#tO,5>[O&MeQ$fO,5>[O&MmQ'[O,5>]OOQO,5>^,5>^OFqQ#tO,5>^O$!gQ#tO,5>_O&MxQ#xO,5>_O&M}Q$fO,5>_O$!gQ#tO,5>`O&NVQ#xO,5>`O&N[Q$fO,5>`O&NdQ#xO,5>aO&NiQ#tO,5>aO$!gQ#tO,5>bO&NnQ#xO,5>bO&NsQ$fO,5>bOOQO'#Hx'#HxOOQO,5>c,5>cOOQO'#Hz'#HzOOQO,5>e,5>eOOQO'#H|'#H|OOQO,5>g,5>gO&N{Q,UO,5>jOOQO,5>j,5>jOOQO,5>k,5>kOOQO,5>l,5>lOOQO'#IS'#ISO'&XQ#tO,5>mO#L{Q#tO,5>mOOQO,5>p,5>pOFqQ#tO,5>pO'&aQ&jO,5>pOFfQ#tO,5>qO'&iQ#tO,5>qOOQO,5>r,5>rOOQO,5>s,5>sOOQO,5>t,5>tOOQO,5>u,5>uOOQO,5>v,5>vOOQO,5>w,5>wOOQO,5>x,5>xOOQO,5>y,5>yO'&qQ,UO'#H`OOQO,5>z,5>zOOQO,5>{,5>{OOQO,5>|,5>|O'&xQ#tO,5>}O#L{Q#tO,5>}OOQO,5?O,5?OO''QQ#tO,5?OO''YQ#tO,5?OOOQO,5?P,5?PO''eQ#tO,5?PO''mQ#tO,5?PO''xQ#tO'#KiO'(QQ!!^O,5AvOOQO,5Av,5AvOOQO,5?R,5?RO'(YQ#tO,5?ROOQO'#Ij'#IjOOQO,5?T,5?TO$IvQ7[O,5?TO'(_Q7[O'#IkOOQO,5?S,5?SO'(fQ#tO,5?SOOQO-E>f-E>fOOQO,5?Z,5?ZO'(kQ#tO,5?ZOOQO1G3S1G3SOOQO1G4v1G4vOOQO1G4y1G4yO'(pQ#tO'#IwO'-xQ%WO1G0iO'.xQ#tO'#I|OOQO1G5O1G5OO'.}Q#tO'#JROOQO1G5T1G5TO'/SQ#tO'#JVOOQO1G5Y1G5YO'/XQ#tO'#J[OOQO1G5^1G5^O'/^Q#tO,5?|OOQO-E>h-E>hO'/cQ#tO'#JcOOQO1G5c1G5cO'/hQ#tO'#JgOOQO1G5j1G5jO'/mQ#tO'#JkOOQO1G5n1G5nO'/rQ#tO'#JoOOQO1G5r1G5rOOQO1G5v1G5vO'/wQ#tO,5<nOOQO1G5x1G5xO'/|Q#tO1G5xO'0RQ#tO1G5xO'0ZQ#tO1G5xO'0fQ#tO1G5xOOQO1G5{1G5{O'/|Q#tO1G5{O'0qQ#tO1G5{O'0yQ#tO1G5{O'1UQ#tO1G5{OOQO1G6O1G6OO'/|Q#tO1G6OO'1aQ#tO1G6OO'1iQ#tO1G6OO'1tQ#tO1G6OOOQO1G6R1G6RO'/|Q#tO1G6RO'2PQ#tO1G6RO'2XQ#tO1G6RO'2dQ#tO1G6ROOQO1G6U1G6UO'/|Q#tO1G6UO'2oQ#tO1G6UO'2wQ#tO1G6UO'3SQ#tO1G6UOOQO1G6X1G6XO'/|Q#tO1G6XO'3_Q#tO1G6XO'3gQ#tO1G6XO'3rQ#tO1G6XOOQO1G6[1G6[O'/|Q#tO1G6[O'3}Q#tO1G6[O'4VQ#tO1G6[O'4bQ#tO1G6[OOQO1G6_1G6_O'/|Q#tO1G6_O'4mQ#tO1G6_O'4uQ#tO1G6_O'5QQ#tO1G6_OOQO-E>p-E>pOOQO-E>o-E>oOOQO-E>n-E>nOOQO-E>m-E>mOOQO-E>l-E>lOOQO-E>k-E>kOOQO-E>j-E>jOOQO-E>i-E>iOOQ!b1G0k1G0kOOQO1G0{1G0{OOQO1G5Q1G5QOOQO1G5V1G5VOOQO1G5[1G5[OOQ`1G5`1G5`OOQO1G5e1G5eOOQO1G5l1G5lOOQO1G5p1G5pOOQO1G5t1G5tOOQ`-E>]-E>]O'5]Q#tO7+&fO':xQ#tO7+&UO'@eQ#tO7+*kO'EkQ,UO7+*pO'JhQ,UO7+*uO( eQ$UO7+*yO( mQ#tO7++OO(&sQ#tO7++VO(,`Q#tO7++ZO(1{Q#tO7++_O!9ZQ&jO'#EuOOQO,5;d,5;dO(7hQ#tO,5;^OOQO-E>[-E>[OOQ`1G0u1G0uO(7mQ#tO,5?bOOQO-E>Z-E>ZOOQ!Ld1G0n1G0nOOQO7+&_7+&_OOQO7+&k7+&kOFqQ#tO7+&kOOQO7+&l7+&lOFqQ#tO7+&lOOQO7+&m7+&mOFqQ#tO7+&mO(7rQ#tO,5;kOOQO-E>^-E>^OOQO1G1T1G1TOOQO7+&n7+&nO#L{Q#tO7+&nOOQO7+&r7+&rOOQO7+&x7+&xOOQO7+&z7+&zOFqQ#tO7+&zOOQO7+&{7+&{OFqQ#tO7+&{OOQO1G1c1G1cOOQO7+&|7+&|OOQO7+'O7+'OO(7wQ#tO7+'OOFqQ#tO7+'OO#FQQ#tO7+'OOOQO7+'S7+'SOOQO1G1k1G1kOOQO7+'T7+'TOOQO7+'W7+'WOOQO7+'Y7+'YOFqQ#tO7+'YOOQO7+']7+']O#L{Q#tO7+']O(=vQ#tO7+']O(>OQ#tO7+']O(>ZQ#tO7+']OOQO7+'_7+'_O#L{Q#tO7+'_O(>fQ#tO7+'_O(>nQ#tO7+'_O(>yQ#tO7+'_OOQO,5<],5<]O(?UQ#tO,5<^OOQO-E>_-E>_O&G{Q#tO7+'aOOQO7+'a7+'aO(?ZQ#tO7+'aOOQO-E>`-E>`O(?lQ#tO,5<_O(?sQ!NvO'#LYOOQO'#LY'#LYOOQO'#Fw'#FwOOQO'#Fv'#FvO(?{Q#tO'#F|O(@SQ&jO'#GROOQO'#Kc'#KcO(@bQ#tO'#FuOOQO,5<_,5<_O(@iQ#tO,5<_O(@nQ#tO,5<_O(?ZQ#tO7+'aOOQO7+'u7+'uO(@vQ$fO7+'uO(@{Q$fO7+'uOOQO-E>c-E>cOOQO7+'w7+'wO$!gQ#tO7+'wO$!gQ#tO7+'wO(ATQ#tO7+'wOOQO7+'x7+'xO$!gQ#tO7+'xO$!gQ#tO7+'xO(AYQ#tO7+'xO$!gQ#tO7+'yO(A_Q#tO7+'yO&G{Q#tO7+'zOOQO7+'z7+'zO$!gQ#tO7+'zO$!gQ#tO7+'zO(AdQ#tO7+'zO(AiQ#tO'#GiOOQO7+(W7+(WO(ApQ#tO7+(WO(AuQ#tO7+(WO#L{Q#tO7+(WOOQO7+(]7+(]OOQO7+(^7+(^OFqQ#tO7+(^OOQO7+(j7+(jO#L{Q#tO7+(jOOQO7+(k7+(kOFqQ#tO7+(kOOQO7+(l7+(lOFqQ#tO7+(lOOQO1G3X1G3XOFqQ#tO1G3XOOQO1G3Y1G3YOFqQ#tO1G3YO(A}Q#tO1G3YOOQO1G3Z1G3ZOFqQ#tO1G3ZO(BVQ#tO1G3ZOOQO1G3[1G3[OFqQ#tO1G3[O(B_Q#tO1G3[OOQO1G3]1G3]O#L{Q#tO1G3]O(BgQ#tO1G3]OOQO1G3^1G3^O#L{Q#tO1G3^OOQO1G3`1G3`OOQO1G3b1G3bOOQO1G3d1G3dO#L{Q#tO1G3dOOQO1G3e1G3eO$HrQ#tO1G3eO(BoQ#tO1G3eOOQO1G3g1G3gO$HrQ#tO1G3gO(BwQ#tO1G3gOOQO1G3h1G3hO#L{Q#tO1G3hO(CPQ,UO1G3iOFqQ#tO1G3iO#FQQ#tO1G3iO#L{Q#tO1G3iOOQO1G3j1G3jOOQO1G3k1G3kOOQO1G3m1G3mO(H]Q#vO1G3mOOQO1G3n1G3nO4PQ#tO1G3nOOQO1G3p1G3pO4PQ#tO1G3pOOQO1G3r1G3rOFqQ#tO1G3rO#FQQ#tO1G3rOOQO1G3s1G3sOOQO1G3t1G3tO(HbQ#tO1G3tO(HjQ#tO1G3tO(HuQ#tO1G3tO(HuQ#tO1G3tOOQO1G3u1G3uO(IQQ#tO1G3uO(IYQ#tO1G3uO(IeQ#tO1G3uO(IeQ#tO1G3uO(IpQ#tO1G3vO(IpQ#tO1G3vOOQO1G3v1G3vO(JRQ#tO1G3vOOQO1G3w1G3wO(JdQ$fO1G3wO(JlQ'[O1G3wOOQO1G3x1G3xO$!gQ#tO1G3yOOQO1G3y1G3yO(JwQ#tO1G3yO$!gQ#tO1G3yO(J|Q#xO1G3yO$!gQ#tO1G3zOOQO1G3z1G3zO(KRQ#tO1G3zO$!gQ#tO1G3zO(KWQ#xO1G3zO(K]Q#tO1G3{O(KbQ#xO1G3{O$!gQ#tO1G3|O&G{Q#tO1G3|O(KgQ#tO1G3|O$!gQ#tO1G3|O(KlQ#xO1G3|OOQO1G4U1G4UOOQO'#IT'#ITO(KqQ#tO1G4XO#L{Q#tO1G4XO(KyQ#tO1G4XOOQO1G4[1G4[OFqQ#tO1G4[O(LRQ#tO1G4]OOQO1G4]1G4]OFqQ#tO1G4]OOQMh,5=z,5=zO(LZQ#tO,5=zOOQO1G4i1G4iO#L{Q#tO1G4iO(L`Q#tO1G4iOOQO1G4j1G4jOFqQ#tO1G4jO(LhQ#tO1G4jOOQO1G4k1G4kOFqQ#tO1G4kO(LpQ#tO1G4kOOQMh,5AT,5ATO$HrQ#tO,5ATOOQMh-E>g-E>gOOQO1G7b1G7bOOQO1G4m1G4mOOQO1G4o1G4oOOQO,5?V,5?VO$IvQ7[O,5?VOOQO1G4n1G4nOOQO1G4u1G4uO(LxQ-hO1G0iO(M]Q&jO'#EeOOQ!b,5?c,5?cOOQO7+&T7+&TO(MbQ&jO'#IzOOQO,5?h,5?hO(MgQ&jO'#JPOOQO,5?m,5?mO(MlQ&jO'#JUOOQO,5?q,5?qO(MqQ&jO'#JYOOQO,5?v,5?vOOQO1G5h1G5hO(MvQ&jO'#J_OOQO,5?},5?}O(M{Q&jO'#JfOOQO,5@R,5@RO(NQQ&jO'#JjOOQO,5@V,5@VO(NVQ&jO'#JnOOQO,5@Z,5@ZOOQO1G2Y1G2YOOQO7++d7++dO'/|Q#tO7++dO(N[Q#tO7++dO(NdQ#tO7++dOOQO7++g7++gO'/|Q#tO7++gO(NoQ#tO7++gO(NwQ#tO7++gOOQO7++j7++jO'/|Q#tO7++jO) SQ#tO7++jO) [Q#tO7++jOOQO7++m7++mO'/|Q#tO7++mO) gQ#tO7++mO) oQ#tO7++mOOQO7++p7++pO'/|Q#tO7++pO) zQ#tO7++pO)!SQ#tO7++pOOQO7++s7++sO'/|Q#tO7++sO)!_Q#tO7++sO)!gQ#tO7++sOOQO7++v7++vO'/|Q#tO7++vO)!rQ#tO7++vO)!zQ#tO7++vOOQO7++y7++yO'/|Q#tO7++yO)#VQ#tO7++yO)#_Q#tO7++yOOQO1G0x1G0xO)#jQ#tO1G3ROOQO1G4|1G4|OOQO<<JV<<JVOOQO<<JW<<JWOOQO<<JX<<JXOOQO1G1V1G1VOOQO<<JY<<JYOOQO<<Jf<<JfOOQO<<Jg<<JgOOQO<<Jj<<JjO)(mQ#tO<<JjOFqQ#tO<<JjOOQO<<Jt<<JtOOQO<<Jw<<JwO#L{Q#tO<<JwO).lQ#tO<<JwO).tQ#tO<<JwOOQO<<Jy<<JyO#L{Q#tO<<JyO)/PQ#tO<<JyO)/XQ#tO<<JyOOQO1G1x1G1xOOQO<<J{<<J{O&G{Q#tO<<J{OOQO1G1y1G1yO)/dQ#tO1G1yO)/iQ#tO'#KdO)/tQ!NvO,5AtOOQO,5At,5AtOOQO,5<h,5<hO)/|Q#tO,5<hOOQO,5<m,5<mO)0RQ#tO,5<mO)0ZQ#tO,5<mO)0fQ&jO,5<mOOQO-E>a-E>aO)0tQ#tO1G1yO)0{Q#tO<<J{OOQO<<Ka<<KaO)1^Q$fO<<KaO$!gQ#tO<<KcOOQO<<Kc<<KcO$!gQ#tO<<KcO$!gQ#tO<<KdOOQO<<Kd<<KdO$!gQ#tO<<KdO$!gQ#tO<<KeO&G{Q#tO<<KeO$!gQ#tO<<KeOOQO<<Kf<<KfO$!gQ#tO<<KfO&G{Q#tO<<KfO$!gQ#tO<<KfO)1cQ#tO,5=TOOQO<<Kr<<KrO(ApQ#tO<<KrO)1hQ#tO<<KrOOQO<<Kx<<KxOOQO<<LU<<LUOOQO<<LV<<LVOOQO<<LW<<LWOOQO7+(s7+(sOOQO7+(t7+(tOFqQ#tO7+(tOOQO7+(u7+(uOFqQ#tO7+(uOOQO7+(v7+(vOFqQ#tO7+(vOOQO7+(w7+(wO#L{Q#tO7+(wOOQO7+(x7+(xOOQO7+)O7+)OOOQO7+)P7+)PO$HrQ#tO7+)POOQO7+)R7+)RO$HrQ#tO7+)ROOQO7+)S7+)SOOQO7+)T7+)TO)1pQ,UO7+)TOFqQ#tO7+)TO#FQQ#tO7+)TOOQO7+)X7+)XOOQO7+)Y7+)YOOQO7+)[7+)[OOQO7+)^7+)^OFqQ#tO7+)^OOQO7+)`7+)`O#L{Q#tO7+)`O)6|Q#tO7+)`O)7UQ#tO7+)`O)7aQ#tO7+)`OOQO7+)a7+)aO#L{Q#tO7+)aO)7lQ#tO7+)aO)7tQ#tO7+)aO)8PQ#tO7+)aO&G{Q#tO7+)bOOQO7+)b7+)bO)8[Q#tO7+)bO)8[Q#tO7+)bOOQO7+)c7+)cO)8mQ$fO7+)cO)8rQ$fO7+)cOOQO7+)e7+)eO$!gQ#tO7+)eO$!gQ#tO7+)eO)8zQ#tO7+)eOOQO7+)f7+)fO$!gQ#tO7+)fO$!gQ#tO7+)fO)9PQ#tO7+)fO$!gQ#tO7+)gO)9UQ#tO7+)gO&G{Q#tO7+)hOOQO7+)h7+)hO$!gQ#tO7+)hO$!gQ#tO7+)hO)9ZQ#tO7+)hOOQO7+)s7+)sO(ApQ#tO7+)sO)9`Q#tO7+)sO#L{Q#tO7+)sOOQO7+)v7+)vOOQO7+)w7+)wOFqQ#tO7+)wOOQMh1G3f1G3fOOQO7+*T7+*TO#L{Q#tO7+*TOOQO7+*U7+*UOFqQ#tO7+*UOOQO7+*V7+*VOFqQ#tO7+*VOOQMh1G6o1G6oOOQO1G4q1G4qOOQO<= O<= OO'/|Q#tO<= OO)9hQ#tO<= OOOQO<= R<= RO'/|Q#tO<= RO)9pQ#tO<= ROOQO<= U<= UO'/|Q#tO<= UO)9xQ#tO<= UOOQO<= X<= XO'/|Q#tO<= XO):QQ#tO<= XOOQO<= [<= [O'/|Q#tO<= [O):YQ#tO<= [OOQO<= _<= _O'/|Q#tO<= _O):bQ#tO<= _OOQO<= b<= bO'/|Q#tO<= bO):jQ#tO<= bOOQO<= e<= eO'/|Q#tO<= eO):rQ#tO<= eOOQOAN@UAN@UOOQOAN@cAN@cO#L{Q#tOAN@cO):zQ#tOAN@cOOQOAN@eAN@eO#L{Q#tOAN@eO);SQ#tOAN@eOOQOAN@gAN@gOOQO7+'e7+'eO);[Q#tO'#FyOOQ!LQ,5AO,5AOO);cQ#tO,5AOOOQ!LQ-E>b-E>bOOQO1G7`1G7`OOQO1G2S1G2SOOQO1G2X1G2XO'/|Q#tO1G2XO);kQ#tO1G2XO);sQ#tO1G2XO)<OQ#tO1G2XO)<ZQ#tO7+'eO&G{Q#tOAN@gOOQOAN@{AN@{OOQOAN@}AN@}O$!gQ#tOAN@}OOQOANAOANAOO$!gQ#tOANAOO&G{Q#tOANAPOOQOANAPANAPO$!gQ#tOANAPO&G{Q#tOANAQOOQOANAQANAQO$!gQ#tOANAQOOQO1G2o1G2oOOQOANA^ANA^O(ApQ#tOANA^OOQO<<L`<<L`OOQO<<La<<LaOOQO<<Lb<<LbOOQO<<Lc<<LcOOQO<<Lk<<LkOOQO<<Lm<<LmOOQO<<Lo<<LoO)<`Q,UO<<LoOFqQ#tO<<LoOOQO<<Lx<<LxOOQO<<Lz<<LzO#L{Q#tO<<LzO)AlQ#tO<<LzO)AtQ#tO<<LzOOQO<<L{<<L{O#L{Q#tO<<L{O)BPQ#tO<<L{O)BXQ#tO<<L{OOQO<<L|<<L|O&G{Q#tO<<L|O)BdQ#tO<<L|OOQO<<L}<<L}O)BuQ$fO<<L}O$!gQ#tO<<MPOOQO<<MP<<MPO$!gQ#tO<<MPO$!gQ#tO<<MQOOQO<<MQ<<MQO$!gQ#tO<<MQO$!gQ#tO<<MRO&G{Q#tO<<MRO$!gQ#tO<<MROOQO<<MS<<MSO$!gQ#tO<<MSO&G{Q#tO<<MSO$!gQ#tO<<MSOOQO<<M_<<M_O(ApQ#tO<<M_O)BzQ#tO<<M_OOQO<<Mc<<McOOQO<<Mo<<MoOOQO<<Mp<<MpOOQO<<Mq<<MqOOQOANDjANDjO'/|Q#tOANDjOOQOANDmANDmO'/|Q#tOANDmOOQOANDpANDpO'/|Q#tOANDpOOQOANDsANDsO'/|Q#tOANDsOOQOANDvANDvO'/|Q#tOANDvOOQOANDyANDyO'/|Q#tOANDyOOQOAND|AND|O'/|Q#tOAND|OOQOANEPANEPO'/|Q#tOANEPOOQOG25}G25}O#L{Q#tOG25}OOQOG26PG26PO#L{Q#tOG26POOQ!LQ,5<e,5<eO)CSQ#tO,5<eOOQ!LQ1G6j1G6jOOQO7+'s7+'sO'/|Q#tO7+'sO)CXQ#tO7+'sO)CaQ#tO7+'sOOQO<<KP<<KPOOQOG26RG26ROOQOG26iG26iOOQOG26jG26jOOQOG26kG26kO&G{Q#tOG26kOOQOG26lG26lO&G{Q#tOG26lOOQOG26xG26xOOQOANBZANBZOOQOANBfANBfO#L{Q#tOANBfO)ClQ#tOANBfOOQOANBgANBgO#L{Q#tOANBgO)CtQ#tOANBgOOQOANBhANBhO&G{Q#tOANBhOOQOANBiANBiOOQOANBkANBkO$!gQ#tOANBkOOQOANBlANBlO$!gQ#tOANBlO&G{Q#tOANBmOOQOANBmANBmO$!gQ#tOANBmO&G{Q#tOANBnOOQOANBnANBnO$!gQ#tOANBnOOQOANByANByO(ApQ#tOANByOOQOG2:UG2:UOOQOG2:XG2:XOOQOG2:[G2:[OOQOG2:_G2:_OOQOG2:bG2:bOOQOG2:eG2:eOOQOG2:hG2:hOOQOG2:kG2:kOOQOLD+iLD+iOOQOLD+kLD+kOOQ!LQ1G2P1G2POOQO<<K_<<K_O'/|Q#tO<<K_O)C|Q#tO<<K_OOQOLD,VLD,VOOQOLD,WLD,WOOQOG28QG28QO#L{Q#tOG28QOOQOG28RG28RO#L{Q#tOG28ROOQOG28SG28SOOQOG28VG28VOOQOG28WG28WOOQOG28XG28XO&G{Q#tOG28XOOQOG28YG28YO&G{Q#tOG28YOOQOG28eG28eOOQOAN@yAN@yO'/|Q#tOAN@yOOQOLD-lLD-lOOQOLD-mLD-mOOQOLD-sLD-sOOQOLD-tLD-tOOQOG26eG26eO)DUQ#tO,5=gO$M]Q#tO,5:}O)IXQ#tO'#G{OVQ#tO'#Ec",
  stateData: ")Ik~O#QOS~OXQOZiO[jO]kO^lO_hO`!OOaqObSOcXOd[Oe_OfbOgcOhdOieOjfOkgOloOmpOn!cOo!cOprOqsOrtOsuOt!yOu!zOv!{Ow!|Ox!}Oy#OOz#PO{#QO|vO}wO!OxO!PzO!Q`O!RaO!SyO!TYO!UZO!VTO!WUO!XVO!YWO!Z{O![]O!]^O!^|O!_|O!`|O!a|O!b}O!c!PO!d!QO!e!RO!f!SO!g!TO!h!UO!i!VO!j!WO!k!XO!l!YO!m!ZO!n![O!o!]O!p!^O!q!_O!{!gO!}!eO#PmO#T!tO#Y!uO#]PO#b!iO#ePO$d!bO$h!iO$l!aO$q!dO$r!iO$s!iO$t!iO~O#Y#^O~O#Y#kO#]#nO#b#oO~O#Y#kO#]#nO#b#rO~O#Y#kO#]#nO#b#uO~O#Y#kO#]#nO#b#xO~O#Y#yO#]#nO#b#|O~O#Y#yO#]#nO#b$QO~O#Y#yO#b$TO~O#Y#yO#b$WO~O#Y#yO#]#nO#b$[O~O#Y#yO#b$^O~O#Y#yO#b$`O~O#Y$aO#b$cO~O#Y#yO#b$fO)r$eO~O#Y#yO#b$hO~O#Y$aO#b$jO~OP$kO~OQ$lO#]#nO#b$nO~O#Y$oO#]#nO#b$sO~O#Y$oO#]#nO#b$wO~O#Y#kO#]#nO#b${O)r$zO~O#Y#yO#b%OO~O#Y#yO#]#nO#b%SO~O#Y#yO#]#nO#b%SO)r%TO~O#Y#yO#]#nO#b%XO~O#Y#yO#]#nO#b%XO)r%YO~O#]#nOX$oXZ$oX[$oX]$oX^$oX_$oX`$oXa$oXb$oXc$oXd$oXe$oXf$oXg$oXh$oXi$oXj$oXk$oXl$oXm$oXn$oXo$oXp$oXq$oXr$oXs$oX|$oX}$oX!O$oX!P$oX!Q$oX!R$oX!S$oX!T$oX!U$oX!V$oX!W$oX!X$oX!Y$oX!Z$oX![$oX!]$oX!^$oX!_$oX!`$oX!a$oX!b$oX!c$oX!d$oX!e$oX!f$oX!g$oX!h$oX!i$oX!j$oX!k$oX!l$oX!m$oX!n$oX!o$oX!p$oX!q$oX#P$oX#Y$oX#b$oX#e$oX$d$oX$h$oX$l$oX$q$oX$s$oX$t$oXY$oX#Z$oX!|$oX#O$oX~Ot$oXu$oXv$oXw$oXx$oXy$oXz$oX{$oX!{$oX!}$oX#T$oX$r$oX)g$oX)q$oX~P,aOU%[O#b%]O$d%[O~OU%^O~O#Y#kO#b%`O~OU%aO#Y%bO#b%cO~OU%dO#Y%eO#b%fO~O#Y%gO#b%hO~OU%iO#Y%jO#b%kO~O#Y$oO#b%lO~O#Y$oO~O#]#nO#b%tOX%VXZ%VX[%VX]%VX^%VX_%VX`%VXa%VXb%VXc%VXd%VXe%VXf%VXg%VXh%VXi%VXj%VXk%VXl%VXm%VXn%VXo%VXp%VXq%VXr%VXs%VXt%VXu%VXv%VXw%VXx%VXy%VXz%VX{%VX|%VX}%VX!O%VX!P%VX!Q%VX!R%VX!S%VX!T%VX!U%VX!V%VX!W%VX!X%VX!Y%VX!Z%VX![%VX!]%VX!^%VX!_%VX!`%VX!a%VX!b%VX!c%VX!d%VX!e%VX!f%VX!g%VX!h%VX!i%VX!j%VX!k%VX!l%VX!m%VX!n%VX!o%VX!p%VX!q%VX!{%VX!}%VX#P%VX#T%VX#Y%VX#e%VX$d%VX$h%VX$l%VX$q%VX$r%VX$s%VX$t%VX)g%VXY%VX)q%VX#Z%VX!|%VX#O%VX~O#b%uOX%WXZ%WX[%WX]%WX^%WX_%WX`%WXa%WXb%WXc%WXd%WXe%WXf%WXg%WXh%WXi%WXj%WXk%WXl%WXm%WXn%WXo%WXp%WXq%WXr%WXs%WXt%WXu%WXv%WXw%WXx%WXy%WXz%WX{%WX|%WX}%WX!O%WX!P%WX!Q%WX!R%WX!S%WX!T%WX!U%WX!V%WX!W%WX!X%WX!Y%WX!Z%WX![%WX!]%WX!^%WX!_%WX!`%WX!a%WX!b%WX!c%WX!d%WX!e%WX!f%WX!g%WX!h%WX!i%WX!j%WX!k%WX!l%WX!m%WX!n%WX!o%WX!p%WX!q%WX!{%WX!}%WX#P%WX#T%WX#Y%WX#]%WX#e%WX$d%WX$h%WX$l%WX$q%WX$r%WX$s%WX$t%WX)g%WXY%WX)q%WX#Z%WX!|%WX#O%WX~O#b%vOX%XXZ%XX[%XX]%XX^%XX_%XX`%XXa%XXb%XXc%XXd%XXe%XXf%XXg%XXh%XXi%XXj%XXk%XXl%XXm%XXn%XXo%XXp%XXq%XXr%XXs%XXt%XXu%XXv%XXw%XXx%XXy%XXz%XX{%XX|%XX}%XX!O%XX!P%XX!Q%XX!R%XX!S%XX!T%XX!U%XX!V%XX!W%XX!X%XX!Y%XX!Z%XX![%XX!]%XX!^%XX!_%XX!`%XX!a%XX!b%XX!c%XX!d%XX!e%XX!f%XX!g%XX!h%XX!i%XX!j%XX!k%XX!l%XX!m%XX!n%XX!o%XX!p%XX!q%XX!{%XX!}%XX#P%XX#T%XX#Y%XX#]%XX#e%XX$d%XX$h%XX$l%XX$q%XX$r%XX$s%XX$t%XX)g%XXY%XX)q%XX#Z%XX!|%XX#O%XX~O#Y#yO#b%yO~O#Y#kO#b%|O)r%{O~O#Y#yO#]#nO#b&QO~O#Y#kO~O#Y#yO#b&_O~O#Y#kO#]#nO#b&bO~O#Y#kO#]#nO#b&eO~O#Y#kO#]#nOZ%oX[%oX]%oX^%oX_%oX`%oXa%oXb%oXc%oXd%oXe%oXf%oXg%oXh%oXi%oXj%oXk%oXl%oXm%oXn%oXo%oXp%oXq%oXr%oXs%oX|%oX}%oX!O%oX!P%oX!Q%oX!R%oX!S%oX!T%oX!U%oX!V%oX!W%oX!X%oX!Y%oX!Z%oX![%oX!]%oX!^%oX!_%oX!`%oX!a%oX!b%oX!c%oX!d%oX!e%oX!f%oX!g%oX!h%oX!i%oX!j%oX!k%oX!l%oX!m%oX!n%oX!o%oX!p%oX!q%oX!{%oX!}%oX#P%oX#e%oX$d%oX$h%oX$l%oX$q%oX$r%oX$s%oX$t%oX)q%oX#Z%oX~O#b&hOX%oXt%oXu%oXv%oXw%oXx%oXy%oXz%oX{%oX#T%oX)g%oXY%oX~PGfOXQOZ'PO['QO]'RO^'SO_'OO`'dOa'VOb&jOc&oOd&rOe&uOf&xOg&yOh&zOi&{Oj&|Ok&}Ol'TOm'UOn'zOp'WOq'XOr'YOs'ZO|'[O}']O!O'^O!P'`O!Q&vO!R&wO!S'_O!T&pO!U&qO!V&kO!W&lO!X&mO!Y&nO!Z'aO![&sO!]&tO!^'bO!_'bO!`'bO!a'bO!b'cO!c'eO!d'fO!e'gO!f'hO!g'iO!h'jO!i'kO!j'lO!k'mO!l'nO!m'oO!n'pO!o'qO!p'rO!q'sO#P&iO#Y'yO#]'|O#b'|O#e'|O$d'vO$h'|O$l'uO$s'|O$t'|O'`'|O'a'|O'b'|O~O$q(PO~PMcO#O(RO~PMcO!|(UO~PMcOXQOZiO[jO]kO^lO_hO`!OOaqObSOcXOd[Oe_OfbOgcOhdOieOjfOkgOloOmpOn!cOo!cOprOqsOrtOsuO|vO}wO!OxO!PzO!Q`O!RaO!SyO!TYO!UZO!VTO!WUO!XVO!YWO!Z{O![]O!]^O!^|O!_|O!`|O!a|O!b}O!c!PO!d!QO!e!RO!f!SO!g!TO!h!UO!i!VO!j!WO!k!XO!l!YO!m!ZO!n![O!o!]O!p!^O!q!_O!{!gO!}!eO#PmO#T!tO#Y!uO#]PO#b!iO#ePO$d!bO$h!iO$l!aO$q!dO$r!iO$s!iO$t!iO~OY%^P~P!$rOY'tP~PMcOT(`O~OXQOZiO[jO]kO^lO_hO`!OOaqObSOcXOd[Oe_OfbOgcOhdOieOjfOkgOloOmpOn!cOo!cOprOqsOrtOsuO|vO}wO!OxO!PzO!Q`O!RaO!SyO!TYO!UZO!VTO!WUO!XVO!YWO!Z{O![]O!]^O!^|O!_|O!`|O!a|O!b}O!c!PO!d!QO!e!RO!f!SO!g!TO!h!UO!i!VO!j!WO!k!XO!l!YO!m!ZO!n![O!o!]O!p!^O!q!_O!{!gO!}!eO#PmO#T!tO#Y(bO#]PO#b!iO#ePO$d!bO$h!iO$l!aO$q!dO$r!iO$s!iO$t!iO)q(cO~O#Z(jO~PVO#Y(mO#]#nO#b(qO)r(pO~O#Y(mO#]#nO#b(uO)r(tO~O#Y(mO#]#nO#b(yO)r(xO~O#Y(mO#]#nO#b(}O)r(|O~O#Y(mO#]#nO#b)RO)r)QO~O#Y(mO#]#nO#b)VO)r)UO~O#Y(mO#]#nO#b)ZO)r)YO~O#Y(mO#]#nO#b)_O)r)^O~Ot(|Pu(|Pv(|Pw(|Px(|Py(|Pz(|P{(|P)g(|PY(|P#Z(|P~P!$rO)g#SXY#SX#Z#SX~PVO{#QOt(yPu(yPv(yPw(yPx(yPy(yPz(yP)g(yPY(yP#Z(yP~P!$rOz#PO{#QOt(vPu(vPv(vPw(vPx(vPy(vP)g(vPY(vP#Z(vP~P!$rOy#OOz#PO{#QOt(sPu(sPv(sPw(sPx(sP)g(sPY(sP#Z(sP~P!$rOx!}Oy#OOz#PO{#QOt(pPu(pPv(pPw(pP)g(pPY(pP#Z(pP~P!$rOw!|Ox!}Oy#OOz#PO{#QOt(mPu(mPv(mP)g(mPY(mP#Z(mP~P!$rOv!{Ow!|Ox!}Oy#OOz#PO{#QOt(jPu(jP)g(jPY(jP#Z(jP~P!$rOu!zOv!{Ow!|Ox!}Oy#OOz#PO{#QOt(gP)g(gPY(gP#Z(gP~P!$rO!r)yO!s)|O!t)}O!u*OO!v*PO!w*QO!x*RO!y*SO!z*TO#Z)zO#j){O~O#Y#kO#]#nOX#haZ#ha[#ha]#ha^#ha_#ha`#haa#hab#hac#had#hae#haf#hag#hah#hai#haj#hak#hal#ham#han#hao#hap#haq#har#has#hat#hau#hav#haw#hax#hay#haz#ha{#ha|#ha}#ha!O#ha!P#ha!Q#ha!R#ha!S#ha!T#ha!U#ha!V#ha!W#ha!X#ha!Y#ha!Z#ha![#ha!]#ha!^#ha!_#ha!`#ha!a#ha!b#ha!c#ha!d#ha!e#ha!f#ha!g#ha!h#ha!i#ha!j#ha!k#ha!l#ha!m#ha!n#ha!o#ha!p#ha!q#ha!{#ha!}#ha#P#ha#T#ha#b#ha#e#ha$d#ha$h#ha$l#ha$q#ha$r#ha$s#ha$t#ha~O#Y#kO#]#nOX#WaZ#Wa[#Wa]#Wa^#Wa_#Wa`#Waa#Wab#Wac#Wad#Wae#Waf#Wag#Wah#Wai#Waj#Wak#Wal#Wam#Wan#Wao#Wap#Waq#War#Was#Wat#Wau#Wav#Waw#Wax#Way#Waz#Wa{#Wa|#Wa}#Wa!O#Wa!P#Wa!Q#Wa!R#Wa!S#Wa!T#Wa!U#Wa!V#Wa!W#Wa!X#Wa!Y#Wa!Z#Wa![#Wa!]#Wa!^#Wa!_#Wa!`#Wa!a#Wa!b#Wa!c#Wa!d#Wa!e#Wa!f#Wa!g#Wa!h#Wa!i#Wa!j#Wa!k#Wa!l#Wa!m#Wa!n#Wa!o#Wa!p#Wa!q#Wa!{#Wa!}#Wa#P#Wa#T#Wa#b#Wa#e#Wa$d#Wa$h#Wa$l#Wa$q#Wa$r#Wa$s#Wa$t#Wa~O#Y#kO#]#nOX'maY'maZ'ma['ma]'ma^'ma_'ma`'maa'mab'mac'mad'mae'maf'mag'mah'mai'maj'mak'mal'mam'man'mao'map'maq'mar'mas'ma|'ma}'ma!O'ma!P'ma!Q'ma!R'ma!S'ma!T'ma!U'ma!V'ma!W'ma!X'ma!Y'ma!Z'ma!['ma!]'ma!^'ma!_'ma!`'ma!a'ma!b'ma!c'ma!d'ma!e'ma!f'ma!g'ma!h'ma!i'ma!j'ma!k'ma!l'ma!m'ma!n'ma!o'ma!p'ma!q'ma!{'ma!}'ma#P'ma#T'ma#b'ma#e'ma$d'ma$h'ma$l'ma$q'ma$r'ma$s'ma$t'ma~O#Y#kO#]#nOX'raY'raZ'ra['ra]'ra^'ra_'ra`'raa'rab'rac'rad'rae'raf'rag'rah'rai'raj'rak'ral'ram'ran'rap'raq'rar'ras'ra|'ra}'ra!O'ra!P'ra!Q'ra!R'ra!S'ra!T'ra!U'ra!V'ra!W'ra!X'ra!Y'ra!Z'ra!['ra!]'ra!^'ra!_'ra!`'ra!a'ra!b'ra!c'ra!d'ra!e'ra!f'ra!g'ra!h'ra!i'ra!j'ra!k'ra!l'ra!m'ra!n'ra!o'ra!p'ra!q'ra#P'ra#b'ra#e'ra$d'ra$h'ra$l'ra$s'ra$t'ra'`'ra'a'ra'b'ra~O#Y#kO#]#nOX'waY'waZ'wa['wa]'wa^'wa_'wa`'waa'wab'wac'wad'wae'waf'wag'wah'wai'waj'wak'wal'wam'wan'wap'waq'war'was'wa|'wa}'wa!O'wa!P'wa!Q'wa!R'wa!S'wa!T'wa!U'wa!V'wa!W'wa!X'wa!Y'wa!Z'wa!['wa!]'wa!^'wa!_'wa!`'wa!a'wa!b'wa!c'wa!d'wa!e'wa!f'wa!g'wa!h'wa!i'wa!j'wa!k'wa!l'wa!m'wa!n'wa!o'wa!p'wa!q'wa#P'wa#b'wa#e'wa$d'wa$h'wa$l'wa$s'wa$t'wa'`'wa'a'wa'b'wa~O#Y#kO#]#nOT'{a~O#Y#kO#]#nOX(QaZ(Qa[(Qa](Qa^(Qa_(Qa`(Qaa(Qab(Qac(Qad(Qae(Qaf(Qag(Qah(Qai(Qaj(Qak(Qal(Qam(Qan(Qao(Qap(Qaq(Qar(Qas(Qa|(Qa}(Qa!O(Qa!P(Qa!Q(Qa!R(Qa!S(Qa!T(Qa!U(Qa!V(Qa!W(Qa!X(Qa!Y(Qa!Z(Qa![(Qa!](Qa!^(Qa!_(Qa!`(Qa!a(Qa!b(Qa!c(Qa!d(Qa!e(Qa!f(Qa!g(Qa!h(Qa!i(Qa!j(Qa!k(Qa!l(Qa!m(Qa!n(Qa!o(Qa!p(Qa!q(Qa!{(Qa!}(Qa#P(Qa#T(Qa#b(Qa#e(Qa$d(Qa$h(Qa$l(Qa$q(Qa$r(Qa$s(Qa$t(Qa)q(Qa~O#Y#kO#]#nOX(XaZ(Xa[(Xa](Xa^(Xa_(Xa`(Xaa(Xab(Xac(Xad(Xae(Xaf(Xag(Xah(Xai(Xaj(Xak(Xal(Xam(Xan(Xao(Xap(Xaq(Xar(Xas(Xat(Xau(Xav(Xaw(Xax(Xay(Xaz(Xa{(Xa|(Xa}(Xa!O(Xa!P(Xa!Q(Xa!R(Xa!S(Xa!T(Xa!U(Xa!V(Xa!W(Xa!X(Xa!Y(Xa!Z(Xa![(Xa!](Xa!^(Xa!_(Xa!`(Xa!a(Xa!b(Xa!c(Xa!d(Xa!e(Xa!f(Xa!g(Xa!h(Xa!i(Xa!j(Xa!k(Xa!l(Xa!m(Xa!n(Xa!o(Xa!p(Xa!q(Xa!{(Xa!}(Xa#P(Xa#T(Xa#b(Xa#e(Xa$d(Xa$h(Xa$l(Xa$q(Xa$r(Xa$s(Xa$t(Xa~O#Y#kO#]#nOX(]aZ(]a[(]a](]a^(]a_(]a`(]aa(]ab(]ac(]ad(]ae(]af(]ag(]ah(]ai(]aj(]ak(]al(]am(]an(]ao(]ap(]aq(]ar(]as(]at(]au(]av(]aw(]ax(]ay(]az(]a{(]a|(]a}(]a!O(]a!P(]a!Q(]a!R(]a!S(]a!T(]a!U(]a!V(]a!W(]a!X(]a!Y(]a!Z(]a![(]a!](]a!^(]a!_(]a!`(]a!a(]a!b(]a!c(]a!d(]a!e(]a!f(]a!g(]a!h(]a!i(]a!j(]a!k(]a!l(]a!m(]a!n(]a!o(]a!p(]a!q(]a!{(]a!}(]a#P(]a#T(]a#b(]a#e(]a$d(]a$h(]a$l(]a$q(]a$r(]a$s(]a$t(]a~O#Y#kO#]#nOX(aaZ(aa[(aa](aa^(aa_(aa`(aaa(aab(aac(aad(aae(aaf(aag(aah(aai(aaj(aak(aal(aam(aan(aao(aap(aaq(aar(aas(aat(aau(aav(aaw(aax(aay(aaz(aa{(aa|(aa}(aa!O(aa!P(aa!Q(aa!R(aa!S(aa!T(aa!U(aa!V(aa!W(aa!X(aa!Y(aa!Z(aa![(aa!](aa!^(aa!_(aa!`(aa!a(aa!b(aa!c(aa!d(aa!e(aa!f(aa!g(aa!h(aa!i(aa!j(aa!k(aa!l(aa!m(aa!n(aa!o(aa!p(aa!q(aa!{(aa!}(aa#P(aa#T(aa#b(aa#e(aa$d(aa$h(aa$l(aa$q(aa$r(aa$s(aa$t(aa~OY*kO~OXQOZiO[jO]kO^lO_hO`!OOaqObSOcXOd[Oe_OfbOgcOhdOieOjfOkgOloOmpOn!cOo!cOprOqsOrtOsuO|vO}wO!OxO!PzO!Q`O!RaO!SyO!TYO!UZO!VTO!WUO!XVO!YWO!Z{O![]O!]^O!^|O!_|O!`|O!a|O!b}O!c!PO!d!QO!e!RO!f!SO!g!TO!h!UO!i!VO!j!WO!k!XO!l!YO!m!ZO!n![O!o!]O!p!^O!q!_O!{!gO!}!eO#PmO#T*nO#Y*mO#]PO#b!iO#ePO$d!bO$h!iO$l!aO$q!dO$r!iO$s!iO$t!iO)q*nO#Z#dP~OZiO[jO]kO^lO_hO`!OOaqObSOcXOd[Oe_OfbOgcOhdOieOjfOkgOloOmpOn!cOo!cOprOqsOrtOsuO|vO}wO!OxO!PzO!Q`O!RaO!SyO!TYO!UZO!VTO!WUO!XVO!YWO!Z{O![]O!]^O!^|O!_|O!`|O!a|O!b}O!c!PO!d!QO!e!RO!f!SO!g!TO!h!UO!i!VO!j!WO!k!XO!l!YO!m!ZO!n![O!o!]O!p!^O!q!_O!{!gO!}!eO#PmO#Y*rO#b!iO$d!bO$h!iO$l?_O$q!dO$r!iO$s!iO$t!iO)q*sO~O#e#^P~P#APO#Y#kO#]#nO~O#Y#kO#b*xO~O#Y#kO#]#nO#b*xO~O#Y#kO#b*{O~O#Y#kO#]#nO#b*{O~O#Y#kO#b+OO~O#Y#kO#]#nO#b+OO~OZiO[jO]kO^lO_hO`!OOaqObSOcXOd[Oe_OfbOgcOhdOieOjfOkgOloOmpOn!cOo!cOprOqsOrtOsuO|vO}wO!OxO!PzO!Q`O!RaO!SyO!TYO!UZO!VTO!WUO!XVO!YWO!Z{O![]O!]^O!^|O!_|O!`|O!a|O!b}O!c!PO!d!QO!e!RO!f!SO!g!TO!h!UO!i!VO!j!WO!k!XO!l!YO!m!ZO!n![O!o!]O!p!^O!q!_O!{!gO!}!eO#PmO#Y+QO#]PO#b!iO#ePO$d!bO$h!iO$l!aO$q!dO$r!iO$s!iO$t!iO)q+RO#Z#rP~O#Y#yO#b+VO~O#Y#yO#]#nO#b+VO~O#Y#yO~O#Y#yO#]#nO~O#Y#kO#b+`O~O#Y#kO#b+cO~OR+eO~O#Y$aO~O#Y#yO#b+kO~OQ+nO~OQ+nO#]#nO~OR+pO~O#Y$oO#]#nO~O#Y#kO#]#nO#b+wO~O#Y#yO#]#nO#b+{O~O#Y#yO#]#nO#b+}O~O#Y#yO#]#nO#b,QO~O#Y#yO#]#nO#b,SO~O#Y,ZO#],UO#b,XO$h,[O)q,TO~OU,^O$d,^O~OU,_O#b,aO)},`O~O#Y,ZO#]#nO#b,cO$h,[O~OR,gO~OU,hO#Y,iO~OR,lO~OU,mO#Y,nO~OR,oO~O#Y,pO~OR,sO~OU,tO#Y,uO~OS,vO~O#b,wOX%VaZ%Va[%Va]%Va^%Va_%Va`%Vaa%Vab%Vac%Vad%Vae%Vaf%Vag%Vah%Vai%Vaj%Vak%Val%Vam%Van%Vao%Vap%Vaq%Var%Vas%Vat%Vau%Vav%Vaw%Vax%Vay%Vaz%Va{%Va|%Va}%Va!O%Va!P%Va!Q%Va!R%Va!S%Va!T%Va!U%Va!V%Va!W%Va!X%Va!Y%Va!Z%Va![%Va!]%Va!^%Va!_%Va!`%Va!a%Va!b%Va!c%Va!d%Va!e%Va!f%Va!g%Va!h%Va!i%Va!j%Va!k%Va!l%Va!m%Va!n%Va!o%Va!p%Va!q%Va!{%Va!}%Va#P%Va#T%Va#Y%Va#]%Va#e%Va$d%Va$h%Va$l%Va$q%Va$r%Va$s%Va$t%Va)g%VaY%Va)q%Va#Z%Va!|%Va#O%Va~O#Y#yO#b,zO~O#Y#kO)r,}O~O#Y#kO#b-RO~O#Y#yO#b-UO~O#Y#kO#b-XO~O#Y#kO#]#nO#b-XO~O#Y#kO#b-[O~O#Y#kO#]#nO#b-[O~O#Y#kO#]#nOX%oaZ%oa[%oa]%oa^%oa_%oa`%oaa%oab%oac%oad%oae%oaf%oag%oah%oai%oaj%oak%oal%oam%oan%oao%oap%oaq%oar%oas%oat%oau%oav%oaw%oax%oay%oaz%oa{%oa|%oa}%oa!O%oa!P%oa!Q%oa!R%oa!S%oa!T%oa!U%oa!V%oa!W%oa!X%oa!Y%oa!Z%oa![%oa!]%oa!^%oa!_%oa!`%oa!a%oa!b%oa!c%oa!d%oa!e%oa!f%oa!g%oa!h%oa!i%oa!j%oa!k%oa!l%oa!m%oa!n%oa!o%oa!p%oa!q%oa!{%oa!}%oa#P%oa#T%oa#b%oa#e%oa$d%oa$h%oa$l%oa$q%oa$r%oa$s%oa$t%oa)g%oaY%oa)q%oa#Z%oa~O'`$oX'a$oX'b$oX~P,aO#Y#kO#]#nO#b-bO~O#Y#kO#]#nO#b-eO~O#Y#kO#]#nO#b-hO~O#Y#kO#]#nO#b-kO~O#Y#yO#]#nO#b-nO~O#Y#yO#]#nO#b-rO~O#Y#yO#b-uO~O#Y#yO#b-xO~O#Y#yO#]#nO#b-{O~O#Y#yO#b-}O~O#Y#yO#b.PO~O#Y$aO#b.RO~O#Y#yO#b.UO)r.TO~O#Y#yO#b.WO~O#Y$aO#b.YO~OP.ZO~OQ.[O#]#nO#b.^O~O#Y$oO#]#nO#b.bO~O#Y$oO#]#nO#b.fO~O#Y#kO#]#nO#b.jO)r.iO~O#Y#yO#b.lO~O#Y#yO#]#nO#b.oO~O#Y#yO#]#nO#b.oO)r.pO~O#Y#yO#]#nO#b.sO~O#Y#yO#]#nO#b.sO)r.tO~OU.uO#b.vO$d.uO~OU.wO~O#Y#kO#b.yO~OU.zO#Y.{O#b.|O~OU.}O#Y/OO#b/PO~O#Y/QO#b/RO~OU/SO#Y/TO#b/UO~O#]#nO#b/^OX&rXZ&rX[&rX]&rX^&rX_&rX`&rXa&rXb&rXc&rXd&rXe&rXf&rXg&rXh&rXi&rXj&rXk&rXl&rXm&rXn&rXp&rXq&rXr&rXs&rX|&rX}&rX!O&rX!P&rX!Q&rX!R&rX!S&rX!T&rX!U&rX!V&rX!W&rX!X&rX!Y&rX!Z&rX![&rX!]&rX!^&rX!_&rX!`&rX!a&rX!b&rX!c&rX!d&rX!e&rX!f&rX!g&rX!h&rX!i&rX!j&rX!k&rX!l&rX!m&rX!n&rX!o&rX!p&rX!q&rX#P&rX#Y&rX#e&rX$d&rX$h&rX$l&rX$q&rX$s&rX$t&rX'`&rX'a&rX'b&rX#O&rX!|&rXY&rX#Z&rXo&rX~O#b/_OX&sXZ&sX[&sX]&sX^&sX_&sX`&sXa&sXb&sXc&sXd&sXe&sXf&sXg&sXh&sXi&sXj&sXk&sXl&sXm&sXn&sXp&sXq&sXr&sXs&sX|&sX}&sX!O&sX!P&sX!Q&sX!R&sX!S&sX!T&sX!U&sX!V&sX!W&sX!X&sX!Y&sX!Z&sX![&sX!]&sX!^&sX!_&sX!`&sX!a&sX!b&sX!c&sX!d&sX!e&sX!f&sX!g&sX!h&sX!i&sX!j&sX!k&sX!l&sX!m&sX!n&sX!o&sX!p&sX!q&sX#P&sX#Y&sX#]&sX#e&sX$d&sX$h&sX$l&sX$q&sX$s&sX$t&sX'`&sX'a&sX'b&sX#O&sX!|&sXY&sX#Z&sXo&sX~O#b/`OX&tXZ&tX[&tX]&tX^&tX_&tX`&tXa&tXb&tXc&tXd&tXe&tXf&tXg&tXh&tXi&tXj&tXk&tXl&tXm&tXn&tXp&tXq&tXr&tXs&tX|&tX}&tX!O&tX!P&tX!Q&tX!R&tX!S&tX!T&tX!U&tX!V&tX!W&tX!X&tX!Y&tX!Z&tX![&tX!]&tX!^&tX!_&tX!`&tX!a&tX!b&tX!c&tX!d&tX!e&tX!f&tX!g&tX!h&tX!i&tX!j&tX!k&tX!l&tX!m&tX!n&tX!o&tX!p&tX!q&tX#P&tX#Y&tX#]&tX#e&tX$d&tX$h&tX$l&tX$q&tX$s&tX$t&tX'`&tX'a&tX'b&tX#O&tX!|&tXY&tX#Z&tXo&tX~O#Y#yO#b/cO~O#Y#kO#b/fO)r/eO~O#Y/qO~O#Y#yO#b/vO~O#Y#kO#]#nO#b/yO~O#Y#kO#]#nO#b/|O~O)h/}O)i0PO~O#Z0QO~PMcO*P0SO*Q0SO*R0SO*S0SO*T0SO*U0SO*V0SO*W0SO*X0SO*Y0SO*Z0SO*[0SO*]0SO*^0SO*_0SO*`0SO*a0SO*b0SO*c0SO*d0SO*e0SO*f0SO*g0SO*h0SO*i0SO*j0SO*k0SO*l0SO*m0SO*n0SO*o0SO*p0SO*q0SO~O#b0UO~P$IvOo0VO~PMcO$q%rX#O%rX!|%rXY%rX#Z%rXo%rX~PMcO$q0ZO~PMcO$q0]O~O#O(RO~O!|(UO~OY0`O~OY%^X#Z%^X~P!$rOY0bO~OY0dO~OY0fO~OY0hO~OY(TX#Z(TX~P!*YOY0lO~OY0nO~OY0pO~OY0rO~O#Z0tO~O#Y(mO#b0wO~O#Y(mO#]#nO#b0yO~O#Y(mO#]#nO#b0yO)r0zO~O#Y(mO#b0|O~O#Y(mO#]#nO#b1OO~O#Y(mO#]#nO#b1OO)r1PO~O#Y(mO#b1RO~O#Y(mO#]#nO#b1TO~O#Y(mO#]#nO#b1TO)r1UO~O#Y(mO#b1WO~O#Y(mO#]#nO#b1YO~O#Y(mO#]#nO#b1YO)r1ZO~O#Y(mO#b1]O~O#Y(mO#]#nO#b1_O~O#Y(mO#]#nO#b1_O)r1`O~O#Y(mO#b1bO~O#Y(mO#]#nO#b1dO~O#Y(mO#]#nO#b1dO)r1eO~O#Y(mO#b1gO~O#Y(mO#]#nO#b1iO~O#Y(mO#]#nO#b1iO)r1jO~O#Y(mO#b1lO~O#Y(mO#]#nO#b1nO~O#Y(mO#]#nO#b1nO)r1oO~Ot(|Xu(|Xv(|Xw(|Xx(|Xy(|Xz(|X{(|X)g(|XY(|X#Z(|X~P!$rO{#QOt(yXu(yXv(yXw(yXx(yXy(yXz(yX)g(yXY(yX#Z(yX~P!$rOz#PO{#QOt(vXu(vXv(vXw(vXx(vXy(vX)g(vXY(vX#Z(vX~P!$rOy#OOz#PO{#QOt(sXu(sXv(sXw(sXx(sX)g(sXY(sX#Z(sX~P!$rOx!}Oy#OOz#PO{#QOt(pXu(pXv(pXw(pX)g(pXY(pX#Z(pX~P!$rOw!|Ox!}Oy#OOz#PO{#QOt(mXu(mXv(mX)g(mXY(mX#Z(mX~P!$rOv!{Ow!|Ox!}Oy#OOz#PO{#QOt(jXu(jX)g(jXY(jX#Z(jX~P!$rOu!zOv!{Ow!|Ox!}Oy#OOz#PO{#QOt(gX)g(gXY(gX#Z(gX~P!$rO#Z1xO~O#Z1yO~O#Z1zO~O#Z1{O~O#Z1|O~O#Z1}O~O#Z2OO~O#Z2PO~O#Z2QO~O#Z2RO~O#Y#kOX#hiZ#hi[#hi]#hi^#hi_#hi`#hia#hib#hic#hid#hie#hif#hig#hih#hii#hij#hik#hil#him#hin#hio#hip#hiq#hir#his#hit#hiu#hiv#hiw#hix#hiy#hiz#hi{#hi|#hi}#hi!O#hi!P#hi!Q#hi!R#hi!S#hi!T#hi!U#hi!V#hi!W#hi!X#hi!Y#hi!Z#hi![#hi!]#hi!^#hi!_#hi!`#hi!a#hi!b#hi!c#hi!d#hi!e#hi!f#hi!g#hi!h#hi!i#hi!j#hi!k#hi!l#hi!m#hi!n#hi!o#hi!p#hi!q#hi!{#hi!}#hi#P#hi#T#hi#]#hi#b#hi#e#hi$d#hi$h#hi$l#hi$q#hi$r#hi$s#hi$t#hi~O#Y#kOX#WiZ#Wi[#Wi]#Wi^#Wi_#Wi`#Wia#Wib#Wic#Wid#Wie#Wif#Wig#Wih#Wii#Wij#Wik#Wil#Wim#Win#Wio#Wip#Wiq#Wir#Wis#Wit#Wiu#Wiv#Wiw#Wix#Wiy#Wiz#Wi{#Wi|#Wi}#Wi!O#Wi!P#Wi!Q#Wi!R#Wi!S#Wi!T#Wi!U#Wi!V#Wi!W#Wi!X#Wi!Y#Wi!Z#Wi![#Wi!]#Wi!^#Wi!_#Wi!`#Wi!a#Wi!b#Wi!c#Wi!d#Wi!e#Wi!f#Wi!g#Wi!h#Wi!i#Wi!j#Wi!k#Wi!l#Wi!m#Wi!n#Wi!o#Wi!p#Wi!q#Wi!{#Wi!}#Wi#P#Wi#T#Wi#]#Wi#b#Wi#e#Wi$d#Wi$h#Wi$l#Wi$q#Wi$r#Wi$s#Wi$t#Wi~O#Y#kOX'miY'miZ'mi['mi]'mi^'mi_'mi`'mia'mib'mic'mid'mie'mif'mig'mih'mii'mij'mik'mil'mim'min'mio'mip'miq'mir'mis'mi|'mi}'mi!O'mi!P'mi!Q'mi!R'mi!S'mi!T'mi!U'mi!V'mi!W'mi!X'mi!Y'mi!Z'mi!['mi!]'mi!^'mi!_'mi!`'mi!a'mi!b'mi!c'mi!d'mi!e'mi!f'mi!g'mi!h'mi!i'mi!j'mi!k'mi!l'mi!m'mi!n'mi!o'mi!p'mi!q'mi!{'mi!}'mi#P'mi#T'mi#]'mi#b'mi#e'mi$d'mi$h'mi$l'mi$q'mi$r'mi$s'mi$t'mi~O#Y#kOX'riY'riZ'ri['ri]'ri^'ri_'ri`'ria'rib'ric'rid'rie'rif'rig'rih'rii'rij'rik'ril'rim'rin'rip'riq'rir'ris'ri|'ri}'ri!O'ri!P'ri!Q'ri!R'ri!S'ri!T'ri!U'ri!V'ri!W'ri!X'ri!Y'ri!Z'ri!['ri!]'ri!^'ri!_'ri!`'ri!a'ri!b'ri!c'ri!d'ri!e'ri!f'ri!g'ri!h'ri!i'ri!j'ri!k'ri!l'ri!m'ri!n'ri!o'ri!p'ri!q'ri#P'ri#]'ri#b'ri#e'ri$d'ri$h'ri$l'ri$s'ri$t'ri'`'ri'a'ri'b'ri~O#Y#kOX'wiY'wiZ'wi['wi]'wi^'wi_'wi`'wia'wib'wic'wid'wie'wif'wig'wih'wii'wij'wik'wil'wim'win'wip'wiq'wir'wis'wi|'wi}'wi!O'wi!P'wi!Q'wi!R'wi!S'wi!T'wi!U'wi!V'wi!W'wi!X'wi!Y'wi!Z'wi!['wi!]'wi!^'wi!_'wi!`'wi!a'wi!b'wi!c'wi!d'wi!e'wi!f'wi!g'wi!h'wi!i'wi!j'wi!k'wi!l'wi!m'wi!n'wi!o'wi!p'wi!q'wi#P'wi#]'wi#b'wi#e'wi$d'wi$h'wi$l'wi$s'wi$t'wi'`'wi'a'wi'b'wi~O#Y#kOT'{i~O#Y#kOX(QiZ(Qi[(Qi](Qi^(Qi_(Qi`(Qia(Qib(Qic(Qid(Qie(Qif(Qig(Qih(Qii(Qij(Qik(Qil(Qim(Qin(Qio(Qip(Qiq(Qir(Qis(Qi|(Qi}(Qi!O(Qi!P(Qi!Q(Qi!R(Qi!S(Qi!T(Qi!U(Qi!V(Qi!W(Qi!X(Qi!Y(Qi!Z(Qi![(Qi!](Qi!^(Qi!_(Qi!`(Qi!a(Qi!b(Qi!c(Qi!d(Qi!e(Qi!f(Qi!g(Qi!h(Qi!i(Qi!j(Qi!k(Qi!l(Qi!m(Qi!n(Qi!o(Qi!p(Qi!q(Qi!{(Qi!}(Qi#P(Qi#T(Qi#](Qi#b(Qi#e(Qi$d(Qi$h(Qi$l(Qi$q(Qi$r(Qi$s(Qi$t(Qi)q(Qi~O#Y#kOX(XiZ(Xi[(Xi](Xi^(Xi_(Xi`(Xia(Xib(Xic(Xid(Xie(Xif(Xig(Xih(Xii(Xij(Xik(Xil(Xim(Xin(Xio(Xip(Xiq(Xir(Xis(Xit(Xiu(Xiv(Xiw(Xix(Xiy(Xiz(Xi{(Xi|(Xi}(Xi!O(Xi!P(Xi!Q(Xi!R(Xi!S(Xi!T(Xi!U(Xi!V(Xi!W(Xi!X(Xi!Y(Xi!Z(Xi![(Xi!](Xi!^(Xi!_(Xi!`(Xi!a(Xi!b(Xi!c(Xi!d(Xi!e(Xi!f(Xi!g(Xi!h(Xi!i(Xi!j(Xi!k(Xi!l(Xi!m(Xi!n(Xi!o(Xi!p(Xi!q(Xi!{(Xi!}(Xi#P(Xi#T(Xi#](Xi#b(Xi#e(Xi$d(Xi$h(Xi$l(Xi$q(Xi$r(Xi$s(Xi$t(Xi~O#Y#kOX(]iZ(]i[(]i](]i^(]i_(]i`(]ia(]ib(]ic(]id(]ie(]if(]ig(]ih(]ii(]ij(]ik(]il(]im(]in(]io(]ip(]iq(]ir(]is(]it(]iu(]iv(]iw(]ix(]iy(]iz(]i{(]i|(]i}(]i!O(]i!P(]i!Q(]i!R(]i!S(]i!T(]i!U(]i!V(]i!W(]i!X(]i!Y(]i!Z(]i![(]i!](]i!^(]i!_(]i!`(]i!a(]i!b(]i!c(]i!d(]i!e(]i!f(]i!g(]i!h(]i!i(]i!j(]i!k(]i!l(]i!m(]i!n(]i!o(]i!p(]i!q(]i!{(]i!}(]i#P(]i#T(]i#](]i#b(]i#e(]i$d(]i$h(]i$l(]i$q(]i$r(]i$s(]i$t(]i~O#Y#kOX(aiZ(ai[(ai](ai^(ai_(ai`(aia(aib(aic(aid(aie(aif(aig(aih(aii(aij(aik(ail(aim(ain(aio(aip(aiq(air(ais(ait(aiu(aiv(aiw(aix(aiy(aiz(ai{(ai|(ai}(ai!O(ai!P(ai!Q(ai!R(ai!S(ai!T(ai!U(ai!V(ai!W(ai!X(ai!Y(ai!Z(ai![(ai!](ai!^(ai!_(ai!`(ai!a(ai!b(ai!c(ai!d(ai!e(ai!f(ai!g(ai!h(ai!i(ai!j(ai!k(ai!l(ai!m(ai!n(ai!o(ai!p(ai!q(ai!{(ai!}(ai#P(ai#T(ai#](ai#b(ai#e(ai$d(ai$h(ai$l(ai$q(ai$r(ai$s(ai$t(ai~O#Y2_O~OXQOZiO[jO]kO^lO_hO`!OOaqObSOcXOd[Oe_OfbOgcOhdOieOjfOkgOloOmpOn!cOo!cOprOqsOrtOsuO|vO}wO!OxO!PzO!Q`O!RaO!SyO!TYO!UZO!VTO!WUO!XVO!YWO!Z{O![]O!]^O!^|O!_|O!`|O!a|O!b}O!c!PO!d!QO!e!RO!f!SO!g!TO!h!UO!i!VO!j!WO!k!XO!l!YO!m!ZO!n![O!o!]O!p!^O!q!_O!{!gO!}!eO#PmO#]PO#b!iO#ePO$d!bO$h!iO$l!aO$q!dO$r!iO$s!iO$t!iO~O#T*nO#Y*mO)q*nO#Z#dX~P&0cO#Z2cO~O#Z#^P~P#APO#e#^X#Z#^X~P#APO#e2fO~O#Y#kO#b2iO~O#Y#kO#b2kO~O#Y#kO#b2mO~O#Y+QO)q+RO#Z#rX~P&0fO#Z2pO~O#Y#yO#b2rO~O#Y#kO#b2vO~O#Y#kO#b2xO~O#Z2yO~O#]#nOX$QiZ$Qi[$Qi]$Qi^$Qi_$Qi`$Qia$Qib$Qic$Qid$Qie$Qif$Qig$Qih$Qii$Qij$Qik$Qil$Qim$Qin$Qio$Qip$Qiq$Qir$Qis$Qit$Qiu$Qiv$Qiw$Qix$Qiy$Qiz$Qi{$Qi|$Qi}$Qi!O$Qi!P$Qi!Q$Qi!R$Qi!S$Qi!T$Qi!U$Qi!V$Qi!W$Qi!X$Qi!Y$Qi!Z$Qi![$Qi!]$Qi!^$Qi!_$Qi!`$Qi!a$Qi!b$Qi!c$Qi!d$Qi!e$Qi!f$Qi!g$Qi!h$Qi!i$Qi!j$Qi!k$Qi!l$Qi!m$Qi!n$Qi!o$Qi!p$Qi!q$Qi!{$Qi!}$Qi#P$Qi#T$Qi#Y$Qi#b$Qi#e$Qi$d$Qi$h$Qi$l$Qi$q$Qi$r$Qi$s$Qi$t$Qi)g$QiY$Qi)q$Qi#Z$Qi!|$Qi#O$Qi~OQ3PO~O#Z3QO~O#Y#yO#b3WO~O#Y#yO#]#nO#b3WO~O#Y#yO#]#nO#b3YO~O#Y#yO#b3]O~O#Y#yO#]#nO#b3]O~O#Y#yO#]#nO#b3_O~O)s3aO)t3aO)u3aO)v3aO)w3aO)x3aO)y3aO)z3aO){3aO~O#Y,ZO#],UO#b3dO$h,[O)q,TO~O#Y3hO#b,cO~OX3oOY3oOZiO[jO]kO^lO_hO`!OOaqObSOcXOd[Oe_OfbOgcOhdOieOjfOkgOloOmpOn3oOo3oOprOqsOrtOsuOt3nOu3nOv3nOw3nOx3nOy3nOz3nO{3nO|vO}wO!OxO!PzO!Q`O!RaO!SyO!TYO!UZO!VTO!WUO!XVO!YWO!Z{O![]O!]^O!^|O!_|O!`|O!a|O!b}O!c!PO!d!QO!e!RO!f!SO!g!TO!h!UO!i!VO!j!WO!k!XO!l!YO!m!ZO!n![O!o!]O!p!^O!q!_O!{3oO!|3oO!}3oO#O3oO#PmO#T3oO#Y3mO#]3oO#b3oO#e3oO$d3jO$h3oO$l3iO$q3oO$r3oO$s3oO$t3oO)q3oO~O#Z3qO~P&@gO#Y,ZO#],UO#b3tO$h,[O)q,TO~OU3uO#b3vO~OU3uO#b3vO)}3wO~O#Z3zO~OR3|O~O#Z4OO~OR4QO~O#Z4RO~OR4SO~O#Y,ZO#b,cO$h,[O~O#Z4VO~OR4XO~O#Y4YO#b4[O~O#Y#yO#b4^O~O#Y#kO#b4aO~O#Y#yO#b4cO~O#Y#kO#b4eO~O#Y#kO#b4gO~O#Y#kO#b4kO~O#Y#kO#]#nO#b4kO~O#Y#kO#b4nO~O#Y#kO#]#nO#b4nO~O#Y#kO#b4qO~O#Y#kO#]#nO#b4qO~O#Y#yO#b4tO~O#Y#yO#]#nO#b4tO~O#Y/qO#b4}O~O#Y/qO#b5QO~O#Y#yO#b5XO~OQ5[O~OQ5[O#]#nO~O#Y#kO#]#nO#b5dO~O#Y#yO#]#nO#b5hO~O#Y#yO#]#nO#b5jO~O#Y#yO#]#nO#b5mO~O#Y#yO#]#nO#b5oO~O#Y,ZO#],UO#b5qO$h,[O)q,TO~OU5sO$d5sO~OU5tO#b5vO)}5uO~OR5zO~OU5{O#Y5|O~OR6PO~OU6QO#Y6RO~OR6SO~O#Y6TO~OR6WO~OU6XO#Y6YO~O#b6ZOX&raZ&ra[&ra]&ra^&ra_&ra`&raa&rab&rac&rad&rae&raf&rag&rah&rai&raj&rak&ral&ram&ran&rap&raq&rar&ras&ra|&ra}&ra!O&ra!P&ra!Q&ra!R&ra!S&ra!T&ra!U&ra!V&ra!W&ra!X&ra!Y&ra!Z&ra![&ra!]&ra!^&ra!_&ra!`&ra!a&ra!b&ra!c&ra!d&ra!e&ra!f&ra!g&ra!h&ra!i&ra!j&ra!k&ra!l&ra!m&ra!n&ra!o&ra!p&ra!q&ra#P&ra#Y&ra#]&ra#e&ra$d&ra$h&ra$l&ra$q&ra$s&ra$t&ra'`&ra'a&ra'b&ra#O&ra!|&raY&ra#Z&rao&ra~O#Y#yO#b6^O~O#Y#kO)r6aO~O#Y#kO#b6dO~O#Z6eO~PMcO#Y#yO#b6hO~O#Y#kO#b6kO~O#Y#kO#]#nO#b6kO~O#Y#kO#b6nO~O#Y#kO#]#nO#b6nO~O#Y/qO#b6qO~O)h/}O)i6sO~O#Z6tO~O#b6wO~P$IvOo0VO~O$q6yO~O#Y6{O~OV6}OW6}OX#ViZ#Vi[#Vi]#Vi^#Vi_#Vi`#Via#Vib#Vic#Vid#Vie#Vif#Vig#Vih#Vii#Vij#Vik#Vil#Vim#Vin#Vio#Vip#Viq#Vir#Vis#Vi|#Vi}#Vi!O#Vi!P#Vi!Q#Vi!R#Vi!S#Vi!T#Vi!U#Vi!V#Vi!W#Vi!X#Vi!Y#Vi!Z#Vi![#Vi!]#Vi!^#Vi!_#Vi!`#Vi!a#Vi!b#Vi!c#Vi!d#Vi!e#Vi!f#Vi!g#Vi!h#Vi!i#Vi!j#Vi!k#Vi!l#Vi!m#Vi!n#Vi!o#Vi!p#Vi!q#Vi#P#Vi#Y#Vi#]#Vi#b#Vi#e#Vi$d#Vi$h#Vi$l#Vi$q#Vi$s#Vi$t#ViY#Vi#Z#Vi~Ot#Viu#Viv#Viw#Vix#Viy#Viz#Vi{#Vi!{#Vi!}#Vi#T#Vi$r#Vi)g#Vi)q#Vi~P'(uO#Y7OO~O#Y7QO~O#Y7SO~O#Y7UO~O#Z7WO~O#Y7XO~O#Y7ZO~O#Y7]O~O#Y7_O~O#Z7aO~O#Y(mO~O#Y(mO#b7cO~O#Y(mO#]#nO#b7cO~O#Y(mO#]#nO#b7eO~O#Y(mO#b7gO~O#Y(mO#]#nO#b7gO~O#Y(mO#]#nO#b7iO~O#Y(mO#b7kO~O#Y(mO#]#nO#b7kO~O#Y(mO#]#nO#b7mO~O#Y(mO#b7oO~O#Y(mO#]#nO#b7oO~O#Y(mO#]#nO#b7qO~O#Y(mO#b7sO~O#Y(mO#]#nO#b7sO~O#Y(mO#]#nO#b7uO~O#Y(mO#b7wO~O#Y(mO#]#nO#b7wO~O#Y(mO#]#nO#b7yO~O#Y(mO#b7{O~O#Y(mO#]#nO#b7{O~O#Y(mO#]#nO#b7}O~O#Y(mO#b8PO~O#Y(mO#]#nO#b8PO~O#Y(mO#]#nO#b8RO~O#Y#kOX#hqZ#hq[#hq]#hq^#hq_#hq`#hqa#hqb#hqc#hqd#hqe#hqf#hqg#hqh#hqi#hqj#hqk#hql#hqm#hqn#hqo#hqp#hqq#hqr#hqs#hqt#hqu#hqv#hqw#hqx#hqy#hqz#hq{#hq|#hq}#hq!O#hq!P#hq!Q#hq!R#hq!S#hq!T#hq!U#hq!V#hq!W#hq!X#hq!Y#hq!Z#hq![#hq!]#hq!^#hq!_#hq!`#hq!a#hq!b#hq!c#hq!d#hq!e#hq!f#hq!g#hq!h#hq!i#hq!j#hq!k#hq!l#hq!m#hq!n#hq!o#hq!p#hq!q#hq!{#hq!}#hq#P#hq#T#hq#]#hq#b#hq#e#hq$d#hq$h#hq$l#hq$q#hq$r#hq$s#hq$t#hq~O#Y#kOX#WqZ#Wq[#Wq]#Wq^#Wq_#Wq`#Wqa#Wqb#Wqc#Wqd#Wqe#Wqf#Wqg#Wqh#Wqi#Wqj#Wqk#Wql#Wqm#Wqn#Wqo#Wqp#Wqq#Wqr#Wqs#Wqt#Wqu#Wqv#Wqw#Wqx#Wqy#Wqz#Wq{#Wq|#Wq}#Wq!O#Wq!P#Wq!Q#Wq!R#Wq!S#Wq!T#Wq!U#Wq!V#Wq!W#Wq!X#Wq!Y#Wq!Z#Wq![#Wq!]#Wq!^#Wq!_#Wq!`#Wq!a#Wq!b#Wq!c#Wq!d#Wq!e#Wq!f#Wq!g#Wq!h#Wq!i#Wq!j#Wq!k#Wq!l#Wq!m#Wq!n#Wq!o#Wq!p#Wq!q#Wq!{#Wq!}#Wq#P#Wq#T#Wq#]#Wq#b#Wq#e#Wq$d#Wq$h#Wq$l#Wq$q#Wq$r#Wq$s#Wq$t#Wq~O#Y#kOX'mqY'mqZ'mq['mq]'mq^'mq_'mq`'mqa'mqb'mqc'mqd'mqe'mqf'mqg'mqh'mqi'mqj'mqk'mql'mqm'mqn'mqo'mqp'mqq'mqr'mqs'mq|'mq}'mq!O'mq!P'mq!Q'mq!R'mq!S'mq!T'mq!U'mq!V'mq!W'mq!X'mq!Y'mq!Z'mq!['mq!]'mq!^'mq!_'mq!`'mq!a'mq!b'mq!c'mq!d'mq!e'mq!f'mq!g'mq!h'mq!i'mq!j'mq!k'mq!l'mq!m'mq!n'mq!o'mq!p'mq!q'mq!{'mq!}'mq#P'mq#T'mq#]'mq#b'mq#e'mq$d'mq$h'mq$l'mq$q'mq$r'mq$s'mq$t'mq~O#Y#kOX'rqY'rqZ'rq['rq]'rq^'rq_'rq`'rqa'rqb'rqc'rqd'rqe'rqf'rqg'rqh'rqi'rqj'rqk'rql'rqm'rqn'rqp'rqq'rqr'rqs'rq|'rq}'rq!O'rq!P'rq!Q'rq!R'rq!S'rq!T'rq!U'rq!V'rq!W'rq!X'rq!Y'rq!Z'rq!['rq!]'rq!^'rq!_'rq!`'rq!a'rq!b'rq!c'rq!d'rq!e'rq!f'rq!g'rq!h'rq!i'rq!j'rq!k'rq!l'rq!m'rq!n'rq!o'rq!p'rq!q'rq#P'rq#]'rq#b'rq#e'rq$d'rq$h'rq$l'rq$s'rq$t'rq'`'rq'a'rq'b'rq~O#Y#kOX'wqY'wqZ'wq['wq]'wq^'wq_'wq`'wqa'wqb'wqc'wqd'wqe'wqf'wqg'wqh'wqi'wqj'wqk'wql'wqm'wqn'wqp'wqq'wqr'wqs'wq|'wq}'wq!O'wq!P'wq!Q'wq!R'wq!S'wq!T'wq!U'wq!V'wq!W'wq!X'wq!Y'wq!Z'wq!['wq!]'wq!^'wq!_'wq!`'wq!a'wq!b'wq!c'wq!d'wq!e'wq!f'wq!g'wq!h'wq!i'wq!j'wq!k'wq!l'wq!m'wq!n'wq!o'wq!p'wq!q'wq#P'wq#]'wq#b'wq#e'wq$d'wq$h'wq$l'wq$s'wq$t'wq'`'wq'a'wq'b'wq~O#Y#kOT'{q~O#Y#kOX(QqZ(Qq[(Qq](Qq^(Qq_(Qq`(Qqa(Qqb(Qqc(Qqd(Qqe(Qqf(Qqg(Qqh(Qqi(Qqj(Qqk(Qql(Qqm(Qqn(Qqo(Qqp(Qqq(Qqr(Qqs(Qq|(Qq}(Qq!O(Qq!P(Qq!Q(Qq!R(Qq!S(Qq!T(Qq!U(Qq!V(Qq!W(Qq!X(Qq!Y(Qq!Z(Qq![(Qq!](Qq!^(Qq!_(Qq!`(Qq!a(Qq!b(Qq!c(Qq!d(Qq!e(Qq!f(Qq!g(Qq!h(Qq!i(Qq!j(Qq!k(Qq!l(Qq!m(Qq!n(Qq!o(Qq!p(Qq!q(Qq!{(Qq!}(Qq#P(Qq#T(Qq#](Qq#b(Qq#e(Qq$d(Qq$h(Qq$l(Qq$q(Qq$r(Qq$s(Qq$t(Qq)q(Qq~O#Y#kOX(XqZ(Xq[(Xq](Xq^(Xq_(Xq`(Xqa(Xqb(Xqc(Xqd(Xqe(Xqf(Xqg(Xqh(Xqi(Xqj(Xqk(Xql(Xqm(Xqn(Xqo(Xqp(Xqq(Xqr(Xqs(Xqt(Xqu(Xqv(Xqw(Xqx(Xqy(Xqz(Xq{(Xq|(Xq}(Xq!O(Xq!P(Xq!Q(Xq!R(Xq!S(Xq!T(Xq!U(Xq!V(Xq!W(Xq!X(Xq!Y(Xq!Z(Xq![(Xq!](Xq!^(Xq!_(Xq!`(Xq!a(Xq!b(Xq!c(Xq!d(Xq!e(Xq!f(Xq!g(Xq!h(Xq!i(Xq!j(Xq!k(Xq!l(Xq!m(Xq!n(Xq!o(Xq!p(Xq!q(Xq!{(Xq!}(Xq#P(Xq#T(Xq#](Xq#b(Xq#e(Xq$d(Xq$h(Xq$l(Xq$q(Xq$r(Xq$s(Xq$t(Xq~O#Y#kOX(]qZ(]q[(]q](]q^(]q_(]q`(]qa(]qb(]qc(]qd(]qe(]qf(]qg(]qh(]qi(]qj(]qk(]ql(]qm(]qn(]qo(]qp(]qq(]qr(]qs(]qt(]qu(]qv(]qw(]qx(]qy(]qz(]q{(]q|(]q}(]q!O(]q!P(]q!Q(]q!R(]q!S(]q!T(]q!U(]q!V(]q!W(]q!X(]q!Y(]q!Z(]q![(]q!](]q!^(]q!_(]q!`(]q!a(]q!b(]q!c(]q!d(]q!e(]q!f(]q!g(]q!h(]q!i(]q!j(]q!k(]q!l(]q!m(]q!n(]q!o(]q!p(]q!q(]q!{(]q!}(]q#P(]q#T(]q#](]q#b(]q#e(]q$d(]q$h(]q$l(]q$q(]q$r(]q$s(]q$t(]q~O#Y#kOX(aqZ(aq[(aq](aq^(aq_(aq`(aqa(aqb(aqc(aqd(aqe(aqf(aqg(aqh(aqi(aqj(aqk(aql(aqm(aqn(aqo(aqp(aqq(aqr(aqs(aqt(aqu(aqv(aqw(aqx(aqy(aqz(aq{(aq|(aq}(aq!O(aq!P(aq!Q(aq!R(aq!S(aq!T(aq!U(aq!V(aq!W(aq!X(aq!Y(aq!Z(aq![(aq!](aq!^(aq!_(aq!`(aq!a(aq!b(aq!c(aq!d(aq!e(aq!f(aq!g(aq!h(aq!i(aq!j(aq!k(aq!l(aq!m(aq!n(aq!o(aq!p(aq!q(aq!{(aq!}(aq#P(aq#T(aq#](aq#b(aq#e(aq$d(aq$h(aq$l(aq$q(aq$r(aq$s(aq$t(aq~O#Z8SO~O#Z8UO~O#Z8YO~O#]#nOX$QqZ$Qq[$Qq]$Qq^$Qq_$Qq`$Qqa$Qqb$Qqc$Qqd$Qqe$Qqf$Qqg$Qqh$Qqi$Qqj$Qqk$Qql$Qqm$Qqn$Qqo$Qqp$Qqq$Qqr$Qqs$Qqt$Qqu$Qqv$Qqw$Qqx$Qqy$Qqz$Qq{$Qq|$Qq}$Qq!O$Qq!P$Qq!Q$Qq!R$Qq!S$Qq!T$Qq!U$Qq!V$Qq!W$Qq!X$Qq!Y$Qq!Z$Qq![$Qq!]$Qq!^$Qq!_$Qq!`$Qq!a$Qq!b$Qq!c$Qq!d$Qq!e$Qq!f$Qq!g$Qq!h$Qq!i$Qq!j$Qq!k$Qq!l$Qq!m$Qq!n$Qq!o$Qq!p$Qq!q$Qq!{$Qq!}$Qq#P$Qq#T$Qq#Y$Qq#b$Qq#e$Qq$d$Qq$h$Qq$l$Qq$q$Qq$r$Qq$s$Qq$t$Qq)g$QqY$Qq)q$Qq#Z$Qq!|$Qq#O$Qq~O#Y#yO#b8cO~O#Y#yO#]#nO#b8cO~O#Y#yO#]#nO#b8eO~O#Y#yO#b8gO~O#Y#yO#]#nO#b8gO~O#Y#yO#]#nO#b8iO~O#e8jO~O#Y,ZO#],UO#b8lO$h,[O)q,TO~O#Z8mO~P&@gO)j8oO)k8qO~O#Z8rO~P&@gO#Y(mO#]#nO#b8wO)r8vO~O#Z$iX~P&@gO#Z8mO~O#Y8yO#b,cO~OU8{O~OU8{O#b8|O~O#Z9PO~O#Z9SO~O#Z9VO~O#Z9ZO~O#Z%^P~P!$rO#Y4YO~O#Y4YO#b9^O~O#Y#kO#b9fO~O#Y#kO#b9hO~O#Y#kO#b9jO~O#Y#yO#b9lO~O#Y/qO#b9pO~O#Y/qO#b9rO~O#]#nOX&ViZ&Vi[&Vi]&Vi^&Vi_&Vi`&Via&Vib&Vic&Vid&Vie&Vif&Vig&Vih&Vii&Vij&Vik&Vil&Vim&Vin&Vip&Viq&Vir&Vis&Vi|&Vi}&Vi!O&Vi!P&Vi!Q&Vi!R&Vi!S&Vi!T&Vi!U&Vi!V&Vi!W&Vi!X&Vi!Y&Vi!Z&Vi![&Vi!]&Vi!^&Vi!_&Vi!`&Vi!a&Vi!b&Vi!c&Vi!d&Vi!e&Vi!f&Vi!g&Vi!h&Vi!i&Vi!j&Vi!k&Vi!l&Vi!m&Vi!n&Vi!o&Vi!p&Vi!q&Vi#P&Vi#Y&Vi#b&Vi#e&Vi$d&Vi$h&Vi$l&Vi$q&Vi$s&Vi$t&Vi'`&Vi'a&Vi'b&Vi#O&Vi!|&ViY&Vi#Z&Vio&Vi~OQ9xO~O#Y#yO#b:OO~O#Y#yO#]#nO#b:OO~O#Y#yO#]#nO#b:QO~O#Y#yO#b:TO~O#Y#yO#]#nO#b:TO~O#Y#yO#]#nO#b:VO~O#Y,ZO#],UO#b:XO$h,[O)q,TO~O#Y,ZO#],UO#b:[O$h,[O)q,TO~OU:]O#b:^O~OU:]O#b:^O)}:_O~O#Z:aO~OR:cO~O#Z:eO~OR:gO~O#Z:hO~OR:iO~O#Z:lO~OR:nO~O#Y4YO#b:pO~O#Y#yO#b:rO~O#Y#kO#b:uO~O#Z:vO~O#Y#yO#b:xO~O#Y#kO#b:zO~O#Y#kO#b:|O~O'`#Vi'a#Vi'b#Vi#O#Vi!|#Vi~P'(uO!r)yO~O!s)|O~O!t)}O~O!u*OO~O!v*PO~O!w*QO~O!x*RO~O!y*SO~O!z*TO~O#Y(mO#b;QO~O#Y(mO#]#nO#b;QO~O#Y(mO#b;TO~O#Y(mO#]#nO#b;TO~O#Y(mO#b;WO~O#Y(mO#]#nO#b;WO~O#Y(mO#b;ZO~O#Y(mO#]#nO#b;ZO~O#Y(mO#b;^O~O#Y(mO#]#nO#b;^O~O#Y(mO#b;aO~O#Y(mO#]#nO#b;aO~O#Y(mO#b;dO~O#Y(mO#]#nO#b;dO~O#Y(mO#b;gO~O#Y(mO#]#nO#b;gO~O#]#nOZ%oi[%oi]%oi^%oi_%oi`%oia%oib%oic%oid%oie%oif%oig%oih%oii%oij%oik%oil%oim%oin%oio%oip%oiq%oir%ois%oi|%oi}%oi!O%oi!P%oi!Q%oi!R%oi!S%oi!T%oi!U%oi!V%oi!W%oi!X%oi!Y%oi!Z%oi![%oi!]%oi!^%oi!_%oi!`%oi!a%oi!b%oi!c%oi!d%oi!e%oi!f%oi!g%oi!h%oi!i%oi!j%oi!k%oi!l%oi!m%oi!n%oi!o%oi!p%oi!q%oi!{%oi!}%oi#P%oi#Y%oi#b%oi#e%oi$d%oi$h%oi$l%oi$q%oi$r%oi$s%oi$t%oi)q%oi#Z%oi~O#]#nOX$QyZ$Qy[$Qy]$Qy^$Qy_$Qy`$Qya$Qyb$Qyc$Qyd$Qye$Qyf$Qyg$Qyh$Qyi$Qyj$Qyk$Qyl$Qym$Qyn$Qyo$Qyp$Qyq$Qyr$Qys$Qyt$Qyu$Qyv$Qyw$Qyx$Qyy$Qyz$Qy{$Qy|$Qy}$Qy!O$Qy!P$Qy!Q$Qy!R$Qy!S$Qy!T$Qy!U$Qy!V$Qy!W$Qy!X$Qy!Y$Qy!Z$Qy![$Qy!]$Qy!^$Qy!_$Qy!`$Qy!a$Qy!b$Qy!c$Qy!d$Qy!e$Qy!f$Qy!g$Qy!h$Qy!i$Qy!j$Qy!k$Qy!l$Qy!m$Qy!n$Qy!o$Qy!p$Qy!q$Qy!{$Qy!}$Qy#P$Qy#T$Qy#Y$Qy#b$Qy#e$Qy$d$Qy$h$Qy$l$Qy$q$Qy$r$Qy$s$Qy$t$Qy)g$QyY$Qy)q$Qy#Z$Qy!|$Qy#O$Qy~O#Y#yO#b;kO~O#Y#yO#]#nO#b;kO~O#Y#yO#b;nO~O#Y#yO#]#nO#b;nO~O#Z;qO~O#Y;rO#]#nO#b;tO~O)j8oO)k;vO~O#Z;wO~O#Y(mO#b;yO~O#Y(mO#]#nO#b;{O~O#Y(mO#]#nO#b;{O)r;|O~O#Z;qO~P&@gO#Y,ZO#],UO#b<OO$h,[O)q,TO~OU<PO~O#Z<[O~O#Y4YO#b<^O~O#]#nOX&VqZ&Vq[&Vq]&Vq^&Vq_&Vq`&Vqa&Vqb&Vqc&Vqd&Vqe&Vqf&Vqg&Vqh&Vqi&Vqj&Vqk&Vql&Vqm&Vqn&Vqp&Vqq&Vqr&Vqs&Vq|&Vq}&Vq!O&Vq!P&Vq!Q&Vq!R&Vq!S&Vq!T&Vq!U&Vq!V&Vq!W&Vq!X&Vq!Y&Vq!Z&Vq![&Vq!]&Vq!^&Vq!_&Vq!`&Vq!a&Vq!b&Vq!c&Vq!d&Vq!e&Vq!f&Vq!g&Vq!h&Vq!i&Vq!j&Vq!k&Vq!l&Vq!m&Vq!n&Vq!o&Vq!p&Vq!q&Vq#P&Vq#Y&Vq#b&Vq#e&Vq$d&Vq$h&Vq$l&Vq$q&Vq$s&Vq$t&Vq'`&Vq'a&Vq'b&Vq#O&Vq!|&VqY&Vq#Z&Vqo&Vq~O#Y#yO#b<jO~O#Y#yO#]#nO#b<jO~O#Y#yO#]#nO#b<lO~O#Y#yO#b<nO~O#Y#yO#]#nO#b<nO~O#Y#yO#]#nO#b<pO~O#Y,ZO#],UO#b<rO$h,[O)q,TO~OU<tO~OU<tO#b<uO~O#Z<xO~O#Z<{O~O#Z=OO~O#Z=SO~O#Y4YO#b=UO~O#Y(mO#b=]O~O#Y(mO#b=_O~O#Y(mO#b=aO~O#Y(mO#b=cO~O#Y(mO#b=eO~O#Y(mO#b=gO~O#Y(mO#b=iO~O#Y(mO#b=kO~O#Y#yO#b=mO~O#Y#yO#b=oO~O#Z=pO~P&@gO#Y;rO#]#nO~O#Y(mO#b=tO~O#Y(mO#]#nO#b=tO~O#Y(mO#]#nO#b=vO~O#Z=wO~O#]#nOX&VyZ&Vy[&Vy]&Vy^&Vy_&Vy`&Vya&Vyb&Vyc&Vyd&Vye&Vyf&Vyg&Vyh&Vyi&Vyj&Vyk&Vyl&Vym&Vyn&Vyp&Vyq&Vyr&Vys&Vy|&Vy}&Vy!O&Vy!P&Vy!Q&Vy!R&Vy!S&Vy!T&Vy!U&Vy!V&Vy!W&Vy!X&Vy!Y&Vy!Z&Vy![&Vy!]&Vy!^&Vy!_&Vy!`&Vy!a&Vy!b&Vy!c&Vy!d&Vy!e&Vy!f&Vy!g&Vy!h&Vy!i&Vy!j&Vy!k&Vy!l&Vy!m&Vy!n&Vy!o&Vy!p&Vy!q&Vy#P&Vy#Y&Vy#b&Vy#e&Vy$d&Vy$h&Vy$l&Vy$q&Vy$s&Vy$t&Vy'`&Vy'a&Vy'b&Vy#O&Vy!|&VyY&Vy#Z&Vyo&Vy~O#Y#yO#b>SO~O#Y#yO#]#nO#b>SO~O#Y#yO#b>VO~O#Y#yO#]#nO#b>VO~O#Y,ZO#],UO#b>YO$h,[O)q,TO~OU>ZO~O#Y4YO#b>gO~O#Z>rO~O#Y(mO#b>tO~O#Y(mO#]#nO#b>tO~O#Y#yO#b>yO~O#Y#yO#b>{O~O#Y(mO#b?VO~O#Y#kO#]#nOZ%oa[%oa]%oa^%oa_%oa`%oaa%oab%oac%oad%oae%oaf%oag%oah%oai%oaj%oak%oal%oam%oan%oao%oap%oaq%oar%oas%oa|%oa}%oa!O%oa!P%oa!Q%oa!R%oa!S%oa!T%oa!U%oa!V%oa!W%oa!X%oa!Y%oa!Z%oa![%oa!]%oa!^%oa!_%oa!`%oa!a%oa!b%oa!c%oa!d%oa!e%oa!f%oa!g%oa!h%oa!i%oa!j%oa!k%oa!l%oa!m%oa!n%oa!o%oa!p%oa!q%oa!{%oa!}%oa#P%oa#b%oa#e%oa$d%oa$h%oa$l%oa$q%oa$r%oa$s%oa$t%oa)q%oa#Z%oa~O#b?]O~PGfO$l$d#T'a'`'b$h#b$r#b~",
  goto: "#1n*sPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPP*tP+S,X-Y.]PP.cP:^:d;a<fP=iF_P,R+SFhGiPGoHR<f<f<f<fHUN^Nd<fNi<fNs<fNy<f! P<f<f<f! e<f<f<f<f<f<f! }!!X<f!#T<f<f!#_<f!#k<f!%P<fP!&e!&x!'YP!+z!,Z<^P!,c!,i!-z!,ZPPPP!,Z!/X<f<f<f<f<f<f<f!4]!4`<f!4f<f!4i<f<f<f<f<f!4l!4r!4|!5j<f<f<f<f<f<f<f<f<f<f<f<f<f<f<f<f!5p:d!6m!6p!7Z!7g!7s!7s!7s!7s!7s!7s!8P!7s!8Z!7s!8a!7s!7s!8g!7s!7s!7s!7s!7s!7s!7s!7s!9[!7s!9f!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!9p!7s!9s!7s!9v!7s!7s!7s!7s!7s!9y!:P!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!7s!7g!7Z!7Z!:Z!:g!:sPPP!6m:d!:y!;v:d!;|!<y!=P!=U,X!=[!>]!>c!>f,X!>i!?j!?p!?v,X!?y!@z!AQ,X!AT!BU!B[!B_,X!Bb!Cc!Ci!Cl!Cr!Cw,X!Cz!D{!ER,X!EU!FV!F],X!F`!Ga!Gg!Gj!HY!He!Hp!Hs!IS!Ia!Id!Iw!JW!JZ!Jr!KT!KW!Ks!LW!LZ!Lz!Ma!Md!NX!Np!Ns# l#!V#!Y#!g##k##r##z#%p#%w#&e#(n#(x#)O#+f#+p#,O#,^#,d#,k#,q#,w#,}#-T#-Z#-a#-gPPPPPP#-m#.]#/Q#0PPPPPPPPPPPP#0}P#1VPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPPP#1cQ#UO[#iR!j!p!q!r?`R(k!u!`!tOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#](Y(b(d)a)e)h)k)n)q)t)w4Y?`d'|!d!f!h!l!m'y'{'}(P/qX*n#k(m*m*o!}!sOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`!h!jOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k(Y(b(d(m)a)e)h)k)n)q)t)w*m*o4Y?`e?`!d!f!h!l!m'y'{'}(P/qQ#`QR6|0`Q#mSQ#qTQ#tUQ#wVQ#{WQ$PXQ$Z[Q$mdQ$reQ$vfQ$ygS%RijS%WklS%Zm&iQ%szW%}!P&O'e/gQ&a!^Q&d!_[&f!a&g&h8T?]?_Q(o!yQ(s!zQ(w!{Q({!|Q)P!}Q)T#OQ)X#PQ)]#QQ*W#_Q*Y#`Q*[#aQ*^#bQ*`#cQ*b#dQ*d#eQ*f#fQ*h#gQ*j#hQ*v#oQ*y#rQ*|#uQ+P#xQ+W#|Q+Y$QQ+^$[Q+i$dQ+o$nQ+r$sQ+t$wS+v$z${Q+z%RS+|%S%TQ,P%WS,R%X%Y#d,d%a%d%i,e,h,j,m,q,t.z.}/S3z3{4O4P4R4V4W5x5{5}6Q6U6X8}9P9Q9S9T9V9X9Z:a:b:e:f:h:l:m<R<T<W<Z<v<x<y<{<|=O=Q=S>]>_>b>eQ-S&QQ-Y&bQ-]&eQ-a&jQ-d&kQ-g&lQ-j&mQ-m&nQ-q&oQ-z&rQ.]&zQ.a&{Q.e&|Q.h&}S.n'P'QS.r'R'SQ/]'`Q/x'rQ/{'sS0x(p(qS0}(t(uS1S(x(yS1X(|(}S1^)Q)RS1c)U)VS1h)Y)ZS1m)^)_Q2{+hQ2}+jQ3U+wS3X+{+|Q3Z+}S3^,Q,RQ3`,SQ4i-bQ4l-eQ4o-hQ4r-kQ4u-nQ4w-rQ4{-{Q5V.SQ5].^Q5_.bQ5a.fS5c.i.jQ5g.nS5i.o.pQ5l.rS5n.s.tQ6l/yQ6o/|S7d0y0zS7h1O1PS7l1T1US7p1Y1ZS7t1_1`S7x1d1eS7|1i1jS8Q1n1oQ8^2|Q8`3OS8d3Y3ZS8h3_3`Q8u3nQ9t5UQ9v5WQ9|5dS:P5h5iQ:R5jS:U5m5nQ:W5oQ;R7eQ;U7iQ;X7mQ;[7qQ;_7uQ;b7yQ;e7}Q;h8RQ;i8_Q;l8eQ;o8iQ;s8oS;z8v8wQ<e9uQ<g9wS<k:Q:RS<o:V:WQ=r;tS=u;{;|Q>Q<fQ>T<lQ>W<pR>u=vQ*u#nR2d*r!u!iOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k#n#y(Y(b(d(m)a)e)h)k)n)q)t)w*m*o*r*t+Q+S4Y?`!t!cOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k#n#y(Y(b(d(m)a)e)h)k)n)q)t)w*m*o*r*t+Q+S4Y?`]3l,Z3h3m3p8y;r#R!`OR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k#n#y(Y(b(d(m)a)e)h)k)n)q)t)w*m*o*r*t+Q+S,Z3h3m3p4Y8y;r?`Q#lSQ#pTQ#sUQ#vVQ$xgQ%_qQ%z!OQ&R!QQ&S!RQ&T!SQ&U!TQ&V!UQ&W!VQ&X!WQ&Y!XQ&Z!YQ&[!ZQ&]![Q&`!^Q&c!_Y&f!a&g&h?]?_!r*U#_#`#a#b#c#d#e#f#g#h*V*W*X*Y*Z*[*]*^*_*`*a*b*c*d*e*f*g*h*i*j2T2U2V2W2X2Y2Z2[2]2^S*q#m#oS*w#q#rS*z#t#uS*}#w#xQ+_$]Q+b$_Q+h$dU+u$y$z${Q,b%`S,|%{%|Q-Q&PS-W&a&bS-Z&d&eQ-`&jQ-c&kQ-f&lQ-i&mQ.g&}Q.x'VQ/d'dQ/i'fQ/j'gQ/k'hQ/l'iQ/m'jQ/n'kQ/o'lQ/p'mQ/w'rQ/z'sQ2g*vS2h*x*yS2j*{*|S2l+O+PS2u+`+aS2w+c+dQ2{+iQ2|+jS3T+v+wQ4_,}S4`-P-RS4d-X-YS4f-[-]S4h-a-bS4j-d-eS4m-g-hS4p-j-kQ5U.SU5b.h.i.jQ5w.yS6`/e/fQ6c/hS6j/x/yS6m/{/|Q8V2iQ8W2kQ8X2mQ8[2vQ8]2xQ8^2}Q8_3OQ8a3UQ9`4aQ9b4eQ9c4gQ9d4iS9e4k4lS9g4n4oS9i4q4rQ9t5VQ9u5WS9{5c5dQ:s6aS:t6b6dS:y6k6lS:{6n6oQ;i8`Q<_9fQ<`9hQ<a9jQ<e9vQ<f9wQ<h9|Q=W:uQ=Y:zQ=Z:|R>Q<gQ*p#kQ0u(mR2a*m!}ROR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`Q#_QR2`*kQ#jRQ(X!jQ(g!pQ(h!qQ(i!rR?^?`R*l#jQ#zWW#}X$P$Q+YS$RY$TS$UZ$W`$X[$Z$[&r+^-z-{4{Q$]]Q$_^Q$d`Q$gaW$|h%O'O.l!n%Pij%R%S%T'P'Q+z+{+|+}.n.o.p3W3X3Y3Z5g5h5i5j8c8d8e:O:P:Q:R;k;l<j<k<l=m>S>T>y!n%Ukl%W%X%Y'R'S,P,Q,R,S.r.s.t3]3^3_3`5l5m5n5o8g8h8i:T:U:V:W;n;o<n<o<p=o>V>W>{S%w}%yQ&P!PQ&^!]S+U#{#|Q+a$^Q+d$`Q+f$bS+j$e$fQ+l$hW,x%x,z,{4^Q-P&OQ-T&^Q-V&_Q-l&nW-o&o-q-r4wS-s&p-uS-v&q-xQ-|&sQ.O&tQ.S&vQ.V&wS/a'c/cQ/h'eQ/u'qS2q+V+WQ2z+gQ3O+kS4b-U-VS4s-m-nQ5O-}Q5R.PQ5S.QS5W.T.UQ5Y.WW6[/b6^6_:rQ6b/gQ6g/uQ6i/vQ8Z2rQ9a4cS9k4t4uQ9s5TQ9w5XS:w6h6iQ<b9lR=X:xQ+T#yR2n+QV+R#y+Q+SQ$OXS+X$P$QR2s+YQ$SYR+Z$TQ$VZR+[$WQ$Y[S+]$Z$[Q-y&rQ2t+^S4z-z-{R9n4{Q$b_Q$ibQ+g$cQ+m$jQ.Q&uQ.X&xQ5T.RR5Z.YQ$qeS+q$r$sR3R+rW$pe$r$s+rW$tf$v$w+tQ%mvQ%owQ%qxW._&{.a.b5_W.c&|.e.f5aQ/V'[Q/X']R/Z'^Q$ufS+s$v$wR3S+tQ$}hQ+x%OQ.k'OR5e.lS%QijU+y%R%S%TS.m'P'QW3V+z+{+|+}U5f.n.o.pW8b3W3X3Y3ZW9}5g5h5i5jU;j8c8d8eW<i:O:P:Q:RS=l;k;lU>R<j<k<lQ>p=mS>x>S>TR?W>yS%VklU,O%W%X%YS.q'R'SW3[,P,Q,R,SU5k.r.s.tW8f3]3^3_3`W:S5l5m5n5oU;m8g8h8iW<m:T:U:V:WS=n;n;oU>U<n<o<pQ>q=oS>z>V>WR?X>{l,V%[,W,X,^.u3f3t5p5q5s8z:Z:[<sR3b,Um,V%[,W,X,^.u3f3t5p5q5s8z:Z:[<sQ,]%[Q,f%aQ,k%dQ,r%iU3e,W,X,^S3y,e,hS3},j,mS4T,q,tQ4U,rQ5r.uQ5y.zQ6O.}Q6V/SU8k3d3f3tS9O3z3{S9R4O4PQ9U4RQ9W4TS9Y4V4WU:Y5p5q5sS:`5x5{S:d5}6QS:j6U6XQ:k6VS;p8l8zS<Q8}9PS<S9Q9SS<U9T9VQ<V9US<X9X9ZQ<Y9YU<q:X:Z:[S<w:a:bS<z:e:fQ<}:hQ=P:jS=R:l:mQ=x<OQ=y<RQ=z<TQ={<UQ=|<WQ=}<XQ>O<ZS>X<r<sS>[<v<xS>^<y<{S>`<|=OQ>a<}S>c=Q=SQ>d=RQ>v=|Q>w>OQ>|>YQ>}>]Q?O>_Q?P>`Q?Q>bQ?R>cQ?S>eQ?Y?QR?Z?SQ3r,ZQ8n3hQ8s3mQ;}8yR=q;r]3o,Z3h3m3p8y;rQ;s8oR=r;t!t!cOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k#n#y(Y(b(d(m)a)e)h)k)n)q)t)w*m*o*r*t+Q+S4Y?`d'x!d!f!h!l!m'y'{'}(P/q]3l,Z3h3m3p8y;r#gnOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k#n#y'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o*r*t+Q+S,Z/q3h3m3p4Y8y;r?`Q(n!yQ(r!zQ(v!{Q(z!|Q)O!}Q)S#OQ)W#PQ)[#QU0v(o(p(qU0{(s(t(uU1Q(w(x(yU1V({(|(}U1[)P)Q)RU1a)T)U)VU1f)X)Y)ZU1k)])^)_W7b0w0x0y0zW7f0|0}1O1PW7j1R1S1T1UW7n1W1X1Y1ZW7r1]1^1_1`W7v1b1c1d1eW7z1g1h1i1jW8O1l1m1n1oQ8t3nU;P7c7d7eU;S7g7h7iU;V7k7l7mU;Y7o7p7qU;]7s7t7uU;`7w7x7yU;c7{7|7}U;f8P8Q8RU;x8u8v8wS=[;Q;RS=^;T;US=`;W;XS=b;Z;[S=d;^;_S=f;a;bS=h;d;eS=j;g;hW=s;y;z;{;|Q>h=]Q>i=_Q>j=aQ>k=cQ>l=eQ>m=gQ>n=iQ>o=kU>s=t=u=vS?U>t>uR?[?VR%nvQ%mvR/V'[R%pwR%rxQ%x}R,{%yQ,y%xS4],z,{R9_4^Q4Z,yS9]4[4]Q:o6]S<]9^9_S=T:p:qQ>P<^S>f=U=VR?T>gQ(Z!kR9[4Y!u!cOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k#n#y(Y(b(d(m)a)e)h)k)n)q)t)w*m*o*r*t+Q+S4Y?`R(Q!dQ(O!dQ(T!fQ(W!hS(]!l!mQ0R'yQ0X'{Q0[(PR6f/qe'|!d!f!h!l!m'y'{'}(P/qe'x!d!f!h!l!m'y'{'}(P/qe't!d!f!h!l!m'y'{'}(P/qQ-p&oS4v-q-rR9m4wQ-t&pR4x-uQ-w&qR4y-xQ/r'nQ/s'oQ/t'pQ4|-|Q5P.OQ6p/}S9o4}5OS9q5Q5RQ:}6qQ<c9pR<d9rQ.`&{S5^.a.bR9y5_Q.d&|S5`.e.fR9z5aR/W'[R/Y']R/['^Q/b'cR6_/cQ6]/bS:q6^6_R=V:re'{!d!f!h!l!m'y'{'}(P/qQ0T'zQ6u0UQ6v0VR;O6wQ0W'{R6x0X!u!fOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k#n#y(Y(b(d(m)a)e)h)k)n)q)t)w*m*o*r*t+Q+S4Y?`Q(S!fR0^(T!u!hOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k#n#y(Y(b(d(m)a)e)h)k)n)q)t)w*m*o*r*t+Q+S4Y?`Q(V!hR0_(WV*s#n*r*tQ0a(XR6z?^!}!kOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`Q#aQR7P0bR([!kR0c([!}!lOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`Q#bQR7R0dQ(^!lR(_!mR0e(^!}!mOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`Q#cQR7T0fR0g(_!}!nOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`Q#dQR7V0hR(a!nR0i(a!}!oOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`Q#eQR7Y0lR(f!oQ(e!oR0j(bV(c!o(b(dR0m(f!}!pOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`Q#fQR7[0nR0o(g!}!qOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`Q#gQR7^0pR0q(h!}!rOR!d!f!h!j!k!l!m!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k'y'{'}(P(Y(b(d(m)a)e)h)k)n)q)t)w*m*o/q4Y?`Q#hQR7`0rR0s(i!Z!vOR!j!k!p!q!r!u#R#T#V#W#X#Y#Z#[#](Y)a)e)h)k)n)q)t)w4Y?`c#SOR!j!p!q!r!u#T?`c#]OR!j!p!q!r!u#T?`R)x#]b#SOR!j!p!q!r!u#T?`T)v#])wg#[OR!j!p!q!r!u#T#])w?`R)u#[b#SOR!j!p!q!r!u#T?`S)s#[)tT)v#])wk#ZOR!j!p!q!r!u#T#[#])t)w?`R)r#Zb#SOR!j!p!q!r!u#T?`S)p#Z)qS)s#[)tT)v#])wo#YOR!j!p!q!r!u#T#Z#[#])q)t)w?`R)o#Yb#SOR!j!p!q!r!u#T?`S)m#Y)nS)p#Z)qS)s#[)tT)v#])ws#XOR!j!p!q!r!u#T#Y#Z#[#])n)q)t)w?`R)l#Xb#SOR!j!p!q!r!u#T?`S)j#X)kS)m#Y)nS)p#Z)qS)s#[)tT)v#])ww#WOR!j!p!q!r!u#T#X#Y#Z#[#])k)n)q)t)w?`R)i#Wb#SOR!j!p!q!r!u#T?`S)g#W)hS)j#X)kS)m#Y)nS)p#Z)qS)s#[)tT)v#])w{#VOR!j!p!q!r!u#T#W#X#Y#Z#[#])h)k)n)q)t)w?`R)f#Vb#SOR!j!p!q!r!u#T?`S)d#V)eS)g#W)hS)j#X)kS)m#Y)nS)p#Z)qS)s#[)tT)v#])w!P#ROR!j!p!q!r!u#T#V#W#X#Y#Z#[#])e)h)k)n)q)t)w?`R)b#R`#TOR!j!p!q!r!u?`R)c#Tb!xOR!j!p!q!r!u#T?`S(Y!k4YQ(l(YS)`#R)aS)d#V)eS)g#W)hS)j#X)kS)m#Y)nS)p#Z)qS)s#[)tT)v#])wS*t#n*rR2e*tU*o#k(m*mR2b*oQ*V#_Q*X#`Q*Z#aQ*]#bQ*_#cQ*a#dQ*c#eQ*e#fQ*g#gQ*i#hx2S*V*X*Z*]*_*a*c*e*g*i2T2U2V2W2X2Y2Z2[2]2^Q2T*WQ2U*YQ2V*[Q2W*^Q2X*`Q2Y*bQ2Z*dQ2[*fQ2]*hR2^*jS+S#y+QR2o+SQ,W%[[3c,W3f5p8z:Z<sS3f,X,^Q5p.uQ8z3tS:Z5q5sR<s:[%O,Y%[%a%d%i,W,X,^,e,h,j,m,q,r,t.u.z.}/S3d3f3t3z3{4O4P4R4T4V4W5p5q5s5x5{5}6Q6U6V6X8l8z8}9P9Q9S9T9U9V9X9Y9Z:X:Z:[:a:b:e:f:h:j:l:m<O<R<T<U<W<X<Z<r<s<v<x<y<{<|<}=O=Q=R=S=|>O>Y>]>_>`>b>c>e?Q?SS3g,Y3sR3s,[Y3p,Z3h3m8y;rR8x3pQ8p3iR;u8pQ,e%aQ,j%dQ,q%i!Y3x,e,j,q3{4P4W5x5}6U8}9Q9T9X:b:f:m<R<T<W<Z<v<y<|=Q>]>_>b>eQ3{,hQ4P,mQ4W,tQ5x.zQ5}.}Q6U/SQ8}3zQ9Q4OQ9T4RQ9X4VQ:b5{Q:f6QQ:m6XQ<R9PQ<T9SQ<W9VQ<Z9ZQ<v:aQ<y:eQ<|:hQ=Q:lQ>]<xQ>_<{Q>b=OR>e=SQ&O!PS-O&O/gR/g'eS&g!a?_S-^&g8TQ-_&hR8T?]b'}!d!f!h!l!m'y'{(P/qR0Y'}Q0O'uR6r0OS(d!o(bR0k(dQ)w#]R1w)wQ)t#[R1v)tQ)q#ZR1u)qQ)n#YR1t)nQ)k#XR1s)kQ)h#WR1r)hQ)e#VR1q)eQ)a#RR1p)a!Z!wOR!j!k!p!q!r!u#R#T#V#W#X#Y#Z#[#](Y)a)e)h)k)n)q)t)w4Y?`!Y!vOR!j!k!p!q!r!u#R#T#V#W#X#Y#Z#[#](Y)a)e)h)k)n)q)t)w4Y?`V(c!o(b(d!nPOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#]#k#y(Y(b(d(m)a)e)h)k)n)q)t)w*m*o+Q+S4Y?`V*s#n*r*t!`!tOR!j!k!o!p!q!r!u#R#T#V#W#X#Y#Z#[#](Y(b(d)a)e)h)k)n)q)t)w4Y?`W*n#k(m*m*oV+R#y+Q+S]3k,Z3h3m3p8y;re'w!d!f!h!l!m'y'{'}(P/qc!xOR!j!p!q!r!u#T?`",
  nodeNames: "⚠ VerbContent LstInlineContent LiteralArgContent SpaceDelimitedLiteralArgContent VerbatimContent Csname TrailingWhitespaceOnly TrailingContent Begin End RefCtrlSeq RefStarrableCtrlSeq CiteCtrlSeq CiteStarrableCtrlSeq LabelCtrlSeq MathTextCtrlSeq HboxCtrlSeq TitleCtrlSeq DocumentClassCtrlSeq UsePackageCtrlSeq HrefCtrlSeq UrlCtrlSeq VerbCtrlSeq LstInlineCtrlSeq IncludeGraphicsCtrlSeq IncludeSvgCtrlSeq CaptionCtrlSeq DefCtrlSeq LetCtrlSeq LeftCtrlSeq RightCtrlSeq NewCommandCtrlSeq RenewCommandCtrlSeq NewEnvironmentCtrlSeq RenewEnvironmentCtrlSeq BookCtrlSeq PartCtrlSeq ChapterCtrlSeq SectionCtrlSeq SubSectionCtrlSeq SubSubSectionCtrlSeq ParagraphCtrlSeq SubParagraphCtrlSeq InputCtrlSeq IncludeCtrlSeq SubfileCtrlSeq ItemCtrlSeq NewTheoremCtrlSeq TheoremStyleCtrlSeq CenteringCtrlSeq BibliographyCtrlSeq BibliographyStyleCtrlSeq AuthorCtrlSeq AffilCtrlSeq AffiliationCtrlSeq DateCtrlSeq MaketitleCtrlSeq TextColorCtrlSeq ColorBoxCtrlSeq HLineCtrlSeq TopRuleCtrlSeq MidRuleCtrlSeq BottomRuleCtrlSeq MultiColumnCtrlSeq ParBoxCtrlSeq TextBoldCtrlSeq TextItalicCtrlSeq TextSmallCapsCtrlSeq TextTeletypeCtrlSeq TextMediumCtrlSeq TextSansSerifCtrlSeq TextSuperscriptCtrlSeq TextSubscriptCtrlSeq TextStrikeOutCtrlSeq EmphasisCtrlSeq UnderlineCtrlSeq SetLengthCtrlSeq FootnoteCtrlSeq EndnoteCtrlSeq DocumentEnvName TabularEnvName EquationEnvName EquationArrayEnvName VerbatimEnvName TikzPictureEnvName FigureEnvName ListEnvName TableEnvName OpenParenCtrlSym CloseParenCtrlSym OpenBracketCtrlSym CloseBracketCtrlSym LineBreakCtrlSym Comment LaTeX Text BlankLine KnownEnvironment DocumentEnvironment BeginEnv EnvNameGroup OpenBrace CloseBrace OptionalArgument OpenBracket ShortOptionalArg Command KnownCommand Title Whitespace TextArgument LongArg CloseBracket NonEmptyGroup Environment BeginEnv EnvNameGroup EnvName Content EndEnv Author Affil Affiliation Date ShortTextArgument ShortArg NonEmptyGroup DocumentClass DocumentClassArgument BibliographyCommand BibliographyArgument BibliographyStyleCommand BibliographyStyleArgument UsePackage PackageArgument TextColorCommand ColorBoxCommand HrefCommand UrlArgument NewTheoremCommand TheoremStyleCommand UrlCommand VerbCommand LstInlineCommand IncludeGraphics IncludeGraphicsArgument FilePathArgument IncludeSvg IncludeSvgArgument Caption Label LabelArgument Ref RefArgument Cite BibKeyArgument Def CtrlSym MacroParameter OptionalMacroParameter DefinitionArgument NewLine DefinitionFragment DefinitionFragmentCommand DefinitionFragmentUnknownCommand CtrlSeq DefinitionFragmentArgument KnownCtrlSym LineBreak Group Dollar Normal Ampersand Tilde SectioningCommand SectioningArgument Let Hbox NewCommand RenewCommand NewEnvironment RenewEnvironment Input InputArgument BareFilePathArgument Include IncludeArgument Subfile SubfileArgument Centering Item Maketitle HorizontalLine MultiColumn SpanArgument ColumnArgument TabularArgument TabularContent MathTextCommand ParBoxCommand TextBoldCommand TextItalicCommand TextSmallCapsCommand TextTeletypeCommand TextMediumCommand TextSansSerifCommand TextSuperscriptCommand TextSubscriptCommand StrikeOutCommand EmphasisCommand UnderlineCommand SetLengthCommand FootnoteCommand EndnoteCommand UnknownCommand DollarMath InlineMath Math MathCommand KnownCommand Title Author Affil Affiliation Date DocumentClass DocumentClassArgument BibliographyCommand BibliographyArgument BibliographyStyleCommand BibliographyStyleArgument UsePackage TextColorCommand MathArgument ColorBoxCommand HrefCommand NewTheoremCommand TheoremStyleCommand UrlCommand VerbCommand LstInlineCommand IncludeGraphics IncludeGraphicsArgument IncludeSvg IncludeSvgArgument Caption Label Ref Cite Def Let Hbox NewCommand RenewCommand NewEnvironment RenewEnvironment Input InputArgument Include IncludeArgument Subfile SubfileArgument Centering Item Maketitle HorizontalLine MultiColumn SpanArgument ColumnArgument MathTextCommand ParBoxCommand TextBoldCommand TextItalicCommand TextSmallCapsCommand TextTeletypeCommand TextMediumCommand TextSansSerifCommand TextSuperscriptCommand TextSubscriptCommand StrikeOutCommand EmphasisCommand UnderlineCommand SetLengthCommand FootnoteCommand EndnoteCommand MathUnknownCommand Group MathDelimitedGroup MathOpening MathDelimiter MathClosing MathSpecialChar Number MathChar DisplayMath BracketMath OpenBracketMath CloseBracketMath ParenMath OpenParenMath CloseParenMath NonEmptyGroup EndEnv TabularEnvironment BeginEnv EnvNameGroup Content EndEnv EquationEnvironment BeginEnv EnvNameGroup Content EndEnv EquationArrayEnvironment BeginEnv EnvNameGroup EndEnv VerbatimEnvironment BeginEnv EnvNameGroup Content EndEnv TikzPictureEnvironment BeginEnv EnvNameGroup Content TikzPictureContent NonEmptyGroup EndEnv FigureEnvironment BeginEnv EnvNameGroup EndEnv ListEnvironment BeginEnv EnvNameGroup EndEnv TableEnvironment BeginEnv EnvNameGroup EndEnv Group Book SectioningCommand Content Part SectioningCommand Content Chapter SectioningCommand Content Section SectioningCommand Content SubSection SectioningCommand Content SubSubSection SectioningCommand Content Paragraph SectioningCommand Content SubParagraph SectioningCommand Content",
  maxTerm: 448,
  context: _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.elementContext,
  nodeProps: [
    ["group", -10,99,115,304,309,314,318,323,330,334,338,"$Environment",-3,216,296,299,"$MathContainer",-8,343,346,349,352,355,358,361,364,"$Section"],
    ["closedBy", 102,"CloseBrace",105,"CloseBracket",297,"CloseBracketMath",300,"CloseParenMath"],
    ["openedBy", 103,"OpenBrace",113,"OpenBracket",298,"OpenBracketMath",301,"OpenParenMath"]
  ],
  skippedNodes: [0,94],
  repeatNodeCount: 24,
  tokenData: "!Fk~R!ROX$[XY'pYZ'{Zp$[pq'pqs$[st(]tu)]uv)bvw*Rwx$[xy*Wyz-hz{/Y{|+x|}$[}!O+x!O!P0z!P!Q2c!Q!R4T!R!S5w!S!T6v!T!U7u!U!V8t!V!W9s!W!X:r!X!Y;q!Y!Z<p!Z![=o![!^$[!^!_+x!_!`>n!`!a+x!a!c$[!c!}@`!}#OBt#O#PB{#P#Q!Dl#Q#R+x#R#S+x#S#T$[#T#o@`#o#p!Ds#p#q!Dx#q#r!Fa#r#s!Ff#s;'S$[;'S;=`'j<%lO$[T$cd'bS$rPOX$[Zp$[pq%qqs$[st&fwx$[x|%q|}$[}!O%q!O!P$[!P![%q![!^$[!^!a%q!a!}$[#Q#S%q#S#o$[#p#q$[#s;'S$[;'S;=`'j<%lO$[P%vW$rPOX%qZs%qw!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qP&cP;=`<%l%qS&k]'bSOX&fZp&fqt&fwx&f|}&f!O!P&f![!^&f!a!}&f#S#o&f#p#q&f#s;'S&f;'S;=`'d<%lO&fS'gP;=`<%l&fT'mP;=`<%l$[~'uQ#b~XY'ppq'p~(QP$h~YZ(T~(YP#T~YZ(TT(d])qP'bSOX&fZp&fqt&fwx&f|}&f!O!P&f![!^&f!a!}&f#S#o&f#p#q&f#s;'S&f;'S;=`'d<%lO&f~)bO$q~~)gT#Q~OY)bYZ)vZ;'S)b;'S;=`){<%lO)b~){O#Q~~*OP;=`<%l)b~*WO$s~]*ag*RW'`S$rPOX%qZs%qwx%qxy+xyz+xz{+x{|+x|}%q}!O+x!O!P%q!P!Q+x!Q!^%q!^!_+x!_!`+x!`!a+x!a!}%q#Q#R+x#R#S+x#S#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qT,Pg'`S$rPOX%qZs%qwx%qxy+xyz+xz{+x{|+x|}%q}!O+x!O!P%q!P!Q+x!Q!^%q!^!_+x!_!`+x!`!a+x!a!}%q#Q#R+x#R#S+x#S#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%q]-qg*SW'`S$rPOX%qZs%qwx%qxy+xyz+xz{+x{|+x|}%q}!O+x!O!P%q!P!Q+x!Q!^%q!^!_+x!_!`+x!`!a+x!a!}%q#Q#R+x#R#S+x#S#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV/cg)rQ'`S$rPOX%qZs%qwx%qxy+xyz+xz{+x{|+x|}%q}!O+x!O!P%q!P!Q+x!Q!^%q!^!_+x!_!`+x!`!a+x!a!}%q#Q#R+x#R#S+x#S#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%q]1Td*qW'bS$rPOX$[Zp$[pq%qqs$[st&fwx$[x|%q|}$[}!O%q!O!P$[!P![%q![!^$[!^!a%q!a!}$[#Q#S%q#S#o$[#p#q$[#s;'S$[;'S;=`'j<%lO$[]2lg*PW'`S$rPOX%qZs%qwx%qxy+xyz+xz{+x{|+x|}%q}!O+x!O!P%q!P!Q+x!Q!^%q!^!_+x!_!`+x!`!a+x!a!}%q#Q#R+x#R#S+x#S#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qT4[['aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qT5XY'aS$rPOX%qZs%qw!Q%q!Q![5Q![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV6Q[)sQ'aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV7P[)tQ'aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV8O[)uQ'aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV8}[)vQ'aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV9|[)wQ'aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV:{[)xQ'aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV;z[)yQ'aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV<y[)zQ'aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV=x[){Q'aS$rPOX%qZs%qw!O%q!O!P5Q!P!Q%q!Q![4T![!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV>wg)}Q'`S$rPOX%qZs%qwx%qxy+xyz+xz{+x{|+x|}%q}!O+x!O!P%q!P!Q+x!Q!^%q!^!_+x!_!`+x!`!a+x!a!}%q#Q#R+x#R#S+x#S#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%qV@ih#jQ'bS$rPOX$[Zp$[pq%qqs$[st&fwx$[xz%qz{BT{|%q|}$[}!O%q!O!P$[!P![%q![!^$[!^!a%q!a!c$[!c!}@`#Q#S%q#S#T$[#T#o@`#p#q$[#s;'S$[;'S;=`'j<%lO$[RB[W#jQ$rPOX%qZs%qw!}%q#Q#o%q#p#q%q#s;'S%q;'S;=`&`<%lO%q_B{O*TW#]V]COjO!cDp!c!fDu!f!gEQ!g!wDu!w!xHY!x!yM|!y!}Du!}#TDp#T#UDu#U#V! `#V#WDu#W#X!$e#X#`Du#`#a!'m#a#fDu#f#g!2[#g#iDu#i#j!<y#j#k!Bm#k#oDu#o#p!DP#p#q!DW#q#r!D_#r;'SDp;'S;=`!Df<%lODpTDuO$dTTDzQ$lT!c!}Du#T#oDu]EVS$lT!c!}Du#T#cDu#c#dEc#d#oDu]EhS$lT!c!}Du#T#kDu#k#lEt#l#oDu]EyS$lT!c!}Du#T#bDu#b#cFV#c#oDu]F[R$lT!c!}Du#T#UFe#U#oDu]FjS$lT!c!}Du#T#fDu#f#gFv#g#oDu]F{S$lT!c!}Du#T#fDu#f#gGX#g#oDu]G^S$lT!c!}Du#T#cDu#c#dGj#d#oDu]GoS$lT!c!}Du#T#kDu#k#lG{#l#oDu]HSQ*cW$lT!c!}Du#T#oDu]H_S$lT!c!}Du#T#dDu#d#eHk#e#oDu]HpT$lT!c!}Du#T#UIP#U#WDu#W#XJt#X#oDu]IUS$lT!c!}Du#T#fDu#f#gIb#g#oDu]IgS$lT!c!}Du#T#fDu#f#gIs#g#oDu]IxS$lT!c!}Du#T#cDu#c#dJU#d#oDu]JZS$lT!c!}Du#T#kDu#k#lJg#l#oDu]JnQ*bW$lT!c!}Du#T#oDu]JyS$lT!c!}Du#T#cDu#c#dKV#d#oDu]K[S$lT!c!}Du#T#kDu#k#lKh#l#oDu]KmS$lT!c!}Du#T#bDu#b#cKy#c#oDu]LOR$lT!c!}Du#T#ULX#U#oDu]L^S$lT!c!}Du#T#fDu#f#gLj#g#oDu]LoS$lT!c!}Du#T#fDu#f#gL{#g#oDu]MQS$lT!c!}Du#T#cDu#c#dM^#d#oDu]McS$lT!c!}Du#T#kDu#k#lMo#l#oDu]MvQ*eW$lT!c!}Du#T#oDu]NRS$lT!c!}Du#T#XDu#X#YN_#Y#oDu]NdS$lT!c!}Du#T#fDu#f#gNp#g#oDu]NuS$lT!c!}Du#T#hDu#h#i! R#i#oDu]! YQ*lW$lT!c!}Du#T#oDu]! eR$lT!c!}Du#T#U! n#U#oDu]! sS$lT!c!}Du#T#VDu#V#W!!P#W#oDu]!!US$lT!c!}Du#T#_Du#_#`!!b#`#oDu]!!gS$lT!c!}Du#T#gDu#g#h!!s#h#oDu]!!xS$lT!c!}Du#T#`Du#`#a!#U#a#oDu]!#ZR$lT!c!}Du#T#U!#d#U#oDu]!#iS$lT!c!}Du#T#gDu#g#h!#u#h#oDu]!#zS$lT!c!}Du#T#[Du#[#]!$W#]#oDu]!$_Q*`W$lT!c!}Du#T#oDu]!$jS$lT!c!}Du#T#cDu#c#d!$v#d#oDu]!${S$lT!c!}Du#T#kDu#k#l!%X#l#oDu]!%^S$lT!c!}Du#T#bDu#b#c!%j#c#oDu]!%oR$lT!c!}Du#T#U!%x#U#oDu]!%}S$lT!c!}Du#T#fDu#f#g!&Z#g#oDu]!&`S$lT!c!}Du#T#fDu#f#g!&l#g#oDu]!&qS$lT!c!}Du#T#cDu#c#d!&}#d#oDu]!'SS$lT!c!}Du#T#kDu#k#l!'`#l#oDu]!'gQ*fW$lT!c!}Du#T#oDu]!'rZ$lT!c!xDu!x!y!(e!y!}Du#T#U!)w#U#V!+l#V#W!-q#W#YDu#Y#Z!/T#Z#jDu#j#k!0x#k#oDu]!(jS$lT!c!}Du#T#XDu#X#Y!(v#Y#oDu]!({S$lT!c!}Du#T#fDu#f#g!)X#g#oDu]!)^S$lT!c!}Du#T#hDu#h#i!)j#i#oDu]!)qQ*hW$lT!c!}Du#T#oDu]!)|S$lT!c!}Du#T#bDu#b#c!*Y#c#oDu]!*_S$lT!c!}Du#T#ZDu#Z#[!*k#[#oDu]!*pS$lT!c!}Du#T#`Du#`#a!*|#a#oDu]!+RS$lT!c!}Du#T#XDu#X#Y!+_#Y#oDu]!+fQ*^W$lT!c!}Du#T#oDu]!+qS$lT!c!}Du#T#fDu#f#g!+}#g#oDu]!,SR$lT!c!}Du#T#U!,]#U#oDu]!,bS$lT!c!}Du#T#VDu#V#W!,n#W#oDu]!,sU$lT!c!}Du#T#XDu#X#Y!-V#Y#_Du#_#`!-d#`#oDu]!-^Q*mW$lT!c!}Du#T#oDu]!-kQ*oW$lT!c!}Du#T#oDu]!-vS$lT!c!}Du#T#XDu#X#Y!.S#Y#oDu]!.XS$lT!c!}Du#T#]Du#]#^!.e#^#oDu]!.jS$lT!c!}Du#T#`Du#`#a!.v#a#oDu]!.}Q*[W$lT!c!}Du#T#oDu]!/YS$lT!c!}Du#T#`Du#`#a!/f#a#oDu]!/kS$lT!c!}Du#T#cDu#c#d!/w#d#oDu]!/|S$lT!c!}Du#T#cDu#c#d!0Y#d#oDu]!0_S$lT!c!}Du#T#fDu#f#g!0k#g#oDu]!0rQ*YW$lT!c!}Du#T#oDu]!0}S$lT!c!}Du#T#XDu#X#Y!1Z#Y#oDu]!1`S$lT!c!}Du#T#fDu#f#g!1l#g#oDu]!1qS$lT!c!}Du#T#hDu#h#i!1}#i#oDu]!2UQ*gW$lT!c!}Du#T#oDu]!2aZ$lT!c!xDu!x!y!3S!y!}Du#T#U!4f#U#V!6Z#V#W!8`#W#YDu#Y#Z!9r#Z#jDu#j#k!;g#k#oDu]!3XS$lT!c!}Du#T#XDu#X#Y!3e#Y#oDu]!3jS$lT!c!}Du#T#fDu#f#g!3v#g#oDu]!3{S$lT!c!}Du#T#hDu#h#i!4X#i#oDu]!4`Q*iW$lT!c!}Du#T#oDu]!4kS$lT!c!}Du#T#bDu#b#c!4w#c#oDu]!4|S$lT!c!}Du#T#ZDu#Z#[!5Y#[#oDu]!5_S$lT!c!}Du#T#`Du#`#a!5k#a#oDu]!5pS$lT!c!}Du#T#XDu#X#Y!5|#Y#oDu]!6TQ*_W$lT!c!}Du#T#oDu]!6`S$lT!c!}Du#T#fDu#f#g!6l#g#oDu]!6qR$lT!c!}Du#T#U!6z#U#oDu]!7PS$lT!c!}Du#T#VDu#V#W!7]#W#oDu]!7bU$lT!c!}Du#T#XDu#X#Y!7t#Y#_Du#_#`!8R#`#oDu]!7{Q*nW$lT!c!}Du#T#oDu]!8YQ*pW$lT!c!}Du#T#oDu]!8eS$lT!c!}Du#T#XDu#X#Y!8q#Y#oDu]!8vS$lT!c!}Du#T#]Du#]#^!9S#^#oDu]!9XS$lT!c!}Du#T#`Du#`#a!9e#a#oDu]!9lQ*]W$lT!c!}Du#T#oDu]!9wS$lT!c!}Du#T#`Du#`#a!:T#a#oDu]!:YS$lT!c!}Du#T#cDu#c#d!:f#d#oDu]!:kS$lT!c!}Du#T#cDu#c#d!:w#d#oDu]!:|S$lT!c!}Du#T#fDu#f#g!;Y#g#oDu]!;aQ*ZW$lT!c!}Du#T#oDu]!;lS$lT!c!}Du#T#XDu#X#Y!;x#Y#oDu]!;}S$lT!c!}Du#T#fDu#f#g!<Z#g#oDu]!<`S$lT!c!}Du#T#hDu#h#i!<l#i#oDu]!<sQ*jW$lT!c!}Du#T#oDu]!=OS$lT!c!}Du#T#dDu#d#e!=[#e#oDu]!=aT$lT!c!}Du#T#U!=p#U#WDu#W#X!?e#X#oDu]!=uS$lT!c!}Du#T#fDu#f#g!>R#g#oDu]!>WS$lT!c!}Du#T#fDu#f#g!>d#g#oDu]!>iS$lT!c!}Du#T#cDu#c#d!>u#d#oDu]!>zS$lT!c!}Du#T#kDu#k#l!?W#l#oDu]!?_Q*aW$lT!c!}Du#T#oDu]!?jS$lT!c!}Du#T#cDu#c#d!?v#d#oDu]!?{S$lT!c!}Du#T#kDu#k#l!@X#l#oDu]!@^S$lT!c!}Du#T#bDu#b#c!@j#c#oDu]!@oR$lT!c!}Du#T#U!@x#U#oDu]!@}S$lT!c!}Du#T#fDu#f#g!AZ#g#oDu]!A`S$lT!c!}Du#T#fDu#f#g!Al#g#oDu]!AqS$lT!c!}Du#T#cDu#c#d!A}#d#oDu]!BSS$lT!c!}Du#T#kDu#k#l!B`#l#oDu]!BgQ*dW$lT!c!}Du#T#oDu]!BrS$lT!c!}Du#T#XDu#X#Y!CO#Y#oDu]!CTS$lT!c!}Du#T#fDu#f#g!Ca#g#oDu]!CfS$lT!c!}Du#T#hDu#h#i!Cr#i#oDu]!CyQ*kW$lT!c!}Du#T#oDu]!DWO*VW$dT]!D_O*XW$dT]!DfO*WW$dTT!DiP;=`<%lDp]!DsO*UW#eT~!DxO#Y~]!ERd*QW'bS$rPOX$[Zp$[pq%qqs$[st&fwx$[x|%q|}$[}!O%q!O!P$[!P![%q![!^$[!^!a%q!a!}$[#Q#S%q#S#o$[#p#q$[#s;'S$[;'S;=`'j<%lO$[~!FfO#Z~~!FkO$t~",
  tokenizers: [_tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.verbTokenizer, _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.lstinlineTokenizer, _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.literalArgTokenizer, _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.spaceDelimitedLiteralArgTokenizer, _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.verbatimTokenizer, _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.csnameTokenizer, _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.trailingContentTokenizer, 0, 1, 2, 3, _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.argumentListTokenizer, _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.argumentListWithOptionalTokenizer],
  topRules: {"LaTeX":[0,95]},
  specialized: [{term: 166, get: (value, stack) => ((0,_tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.specializeCtrlSeq)(value, stack) << 1), external: _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.specializeCtrlSeq},{term: 118, get: (value, stack) => ((0,_tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.specializeEnvName)(value, stack) << 1), external: _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.specializeEnvName},{term: 158, get: (value, stack) => ((0,_tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.specializeCtrlSym)(value, stack) << 1), external: _tokens_mjs__WEBPACK_IMPORTED_MODULE_1__.specializeCtrlSym}],
  tokenPrec: 18784
})


/***/ },

/***/ "../../frontend/js/features/source-editor/lezer-latex/latex.terms.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   AffilCtrlSeq: () => (/* binding */ AffilCtrlSeq),
/* harmony export */   AffiliationCtrlSeq: () => (/* binding */ AffiliationCtrlSeq),
/* harmony export */   Ampersand: () => (/* binding */ Ampersand),
/* harmony export */   AuthorCtrlSeq: () => (/* binding */ AuthorCtrlSeq),
/* harmony export */   BareFilePathArgument: () => (/* binding */ BareFilePathArgument),
/* harmony export */   Begin: () => (/* binding */ Begin),
/* harmony export */   BibKeyArgument: () => (/* binding */ BibKeyArgument),
/* harmony export */   BibliographyCtrlSeq: () => (/* binding */ BibliographyCtrlSeq),
/* harmony export */   BibliographyStyleCtrlSeq: () => (/* binding */ BibliographyStyleCtrlSeq),
/* harmony export */   BlankLine: () => (/* binding */ BlankLine),
/* harmony export */   Book: () => (/* binding */ Book),
/* harmony export */   BookCtrlSeq: () => (/* binding */ BookCtrlSeq),
/* harmony export */   BottomRuleCtrlSeq: () => (/* binding */ BottomRuleCtrlSeq),
/* harmony export */   BracketMath: () => (/* binding */ BracketMath),
/* harmony export */   CaptionCtrlSeq: () => (/* binding */ CaptionCtrlSeq),
/* harmony export */   CenteringCtrlSeq: () => (/* binding */ CenteringCtrlSeq),
/* harmony export */   Chapter: () => (/* binding */ Chapter),
/* harmony export */   ChapterCtrlSeq: () => (/* binding */ ChapterCtrlSeq),
/* harmony export */   CiteCtrlSeq: () => (/* binding */ CiteCtrlSeq),
/* harmony export */   CiteStarrableCtrlSeq: () => (/* binding */ CiteStarrableCtrlSeq),
/* harmony export */   CloseBrace: () => (/* binding */ CloseBrace),
/* harmony export */   CloseBracket: () => (/* binding */ CloseBracket),
/* harmony export */   CloseBracketCtrlSym: () => (/* binding */ CloseBracketCtrlSym),
/* harmony export */   CloseBracketMath: () => (/* binding */ CloseBracketMath),
/* harmony export */   CloseParenCtrlSym: () => (/* binding */ CloseParenCtrlSym),
/* harmony export */   CloseParenMath: () => (/* binding */ CloseParenMath),
/* harmony export */   ColorBoxCtrlSeq: () => (/* binding */ ColorBoxCtrlSeq),
/* harmony export */   Command: () => (/* binding */ Command),
/* harmony export */   Comment: () => (/* binding */ Comment),
/* harmony export */   Csname: () => (/* binding */ Csname),
/* harmony export */   CtrlSeq: () => (/* binding */ CtrlSeq),
/* harmony export */   CtrlSym: () => (/* binding */ CtrlSym),
/* harmony export */   DateCtrlSeq: () => (/* binding */ DateCtrlSeq),
/* harmony export */   DefCtrlSeq: () => (/* binding */ DefCtrlSeq),
/* harmony export */   DefinitionArgument: () => (/* binding */ DefinitionArgument),
/* harmony export */   DefinitionFragment: () => (/* binding */ DefinitionFragment),
/* harmony export */   DefinitionFragmentArgument: () => (/* binding */ DefinitionFragmentArgument),
/* harmony export */   DefinitionFragmentCommand: () => (/* binding */ DefinitionFragmentCommand),
/* harmony export */   DisplayMath: () => (/* binding */ DisplayMath),
/* harmony export */   DocumentClassCtrlSeq: () => (/* binding */ DocumentClassCtrlSeq),
/* harmony export */   DocumentEnvName: () => (/* binding */ DocumentEnvName),
/* harmony export */   DocumentEnvironment: () => (/* binding */ DocumentEnvironment),
/* harmony export */   Dollar: () => (/* binding */ Dollar),
/* harmony export */   DollarMath: () => (/* binding */ DollarMath),
/* harmony export */   EmphasisCtrlSeq: () => (/* binding */ EmphasisCtrlSeq),
/* harmony export */   End: () => (/* binding */ End),
/* harmony export */   EndnoteCtrlSeq: () => (/* binding */ EndnoteCtrlSeq),
/* harmony export */   EnvName: () => (/* binding */ EnvName),
/* harmony export */   Environment: () => (/* binding */ Environment),
/* harmony export */   EquationArrayEnvName: () => (/* binding */ EquationArrayEnvName),
/* harmony export */   EquationArrayEnvironment: () => (/* binding */ EquationArrayEnvironment),
/* harmony export */   EquationEnvName: () => (/* binding */ EquationEnvName),
/* harmony export */   EquationEnvironment: () => (/* binding */ EquationEnvironment),
/* harmony export */   FigureEnvName: () => (/* binding */ FigureEnvName),
/* harmony export */   FigureEnvironment: () => (/* binding */ FigureEnvironment),
/* harmony export */   FilePathArgument: () => (/* binding */ FilePathArgument),
/* harmony export */   FootnoteCtrlSeq: () => (/* binding */ FootnoteCtrlSeq),
/* harmony export */   HLineCtrlSeq: () => (/* binding */ HLineCtrlSeq),
/* harmony export */   HboxCtrlSeq: () => (/* binding */ HboxCtrlSeq),
/* harmony export */   HrefCtrlSeq: () => (/* binding */ HrefCtrlSeq),
/* harmony export */   IncludeCtrlSeq: () => (/* binding */ IncludeCtrlSeq),
/* harmony export */   IncludeGraphicsCtrlSeq: () => (/* binding */ IncludeGraphicsCtrlSeq),
/* harmony export */   IncludeSvgCtrlSeq: () => (/* binding */ IncludeSvgCtrlSeq),
/* harmony export */   InlineMath: () => (/* binding */ InlineMath),
/* harmony export */   InputCtrlSeq: () => (/* binding */ InputCtrlSeq),
/* harmony export */   ItemCtrlSeq: () => (/* binding */ ItemCtrlSeq),
/* harmony export */   KnownCtrlSym: () => (/* binding */ KnownCtrlSym),
/* harmony export */   KnownEnvironment: () => (/* binding */ KnownEnvironment),
/* harmony export */   LaTeX: () => (/* binding */ LaTeX),
/* harmony export */   LabelArgument: () => (/* binding */ LabelArgument),
/* harmony export */   LabelCtrlSeq: () => (/* binding */ LabelCtrlSeq),
/* harmony export */   LeftCtrlSeq: () => (/* binding */ LeftCtrlSeq),
/* harmony export */   LetCtrlSeq: () => (/* binding */ LetCtrlSeq),
/* harmony export */   LineBreakCtrlSym: () => (/* binding */ LineBreakCtrlSym),
/* harmony export */   ListEnvName: () => (/* binding */ ListEnvName),
/* harmony export */   ListEnvironment: () => (/* binding */ ListEnvironment),
/* harmony export */   LiteralArgContent: () => (/* binding */ LiteralArgContent),
/* harmony export */   LongArg: () => (/* binding */ LongArg),
/* harmony export */   LstInlineContent: () => (/* binding */ LstInlineContent),
/* harmony export */   LstInlineCtrlSeq: () => (/* binding */ LstInlineCtrlSeq),
/* harmony export */   MacroParameter: () => (/* binding */ MacroParameter),
/* harmony export */   MaketitleCtrlSeq: () => (/* binding */ MaketitleCtrlSeq),
/* harmony export */   Math: () => (/* binding */ Math),
/* harmony export */   MathArgument: () => (/* binding */ MathArgument),
/* harmony export */   MathChar: () => (/* binding */ MathChar),
/* harmony export */   MathClosing: () => (/* binding */ MathClosing),
/* harmony export */   MathCommand: () => (/* binding */ MathCommand),
/* harmony export */   MathDelimitedGroup: () => (/* binding */ MathDelimitedGroup),
/* harmony export */   MathDelimiter: () => (/* binding */ MathDelimiter),
/* harmony export */   MathOpening: () => (/* binding */ MathOpening),
/* harmony export */   MathSpecialChar: () => (/* binding */ MathSpecialChar),
/* harmony export */   MathTextCtrlSeq: () => (/* binding */ MathTextCtrlSeq),
/* harmony export */   MidRuleCtrlSeq: () => (/* binding */ MidRuleCtrlSeq),
/* harmony export */   MultiColumnCtrlSeq: () => (/* binding */ MultiColumnCtrlSeq),
/* harmony export */   NewCommandCtrlSeq: () => (/* binding */ NewCommandCtrlSeq),
/* harmony export */   NewEnvironmentCtrlSeq: () => (/* binding */ NewEnvironmentCtrlSeq),
/* harmony export */   NewLine: () => (/* binding */ NewLine),
/* harmony export */   NewTheoremCtrlSeq: () => (/* binding */ NewTheoremCtrlSeq),
/* harmony export */   Normal: () => (/* binding */ Normal),
/* harmony export */   Number: () => (/* binding */ Number),
/* harmony export */   OpenBrace: () => (/* binding */ OpenBrace),
/* harmony export */   OpenBracket: () => (/* binding */ OpenBracket),
/* harmony export */   OpenBracketCtrlSym: () => (/* binding */ OpenBracketCtrlSym),
/* harmony export */   OpenBracketMath: () => (/* binding */ OpenBracketMath),
/* harmony export */   OpenParenCtrlSym: () => (/* binding */ OpenParenCtrlSym),
/* harmony export */   OpenParenMath: () => (/* binding */ OpenParenMath),
/* harmony export */   OptionalArgument: () => (/* binding */ OptionalArgument),
/* harmony export */   OptionalMacroParameter: () => (/* binding */ OptionalMacroParameter),
/* harmony export */   PackageArgument: () => (/* binding */ PackageArgument),
/* harmony export */   ParBoxCtrlSeq: () => (/* binding */ ParBoxCtrlSeq),
/* harmony export */   Paragraph: () => (/* binding */ Paragraph),
/* harmony export */   ParagraphCtrlSeq: () => (/* binding */ ParagraphCtrlSeq),
/* harmony export */   ParenMath: () => (/* binding */ ParenMath),
/* harmony export */   Part: () => (/* binding */ Part),
/* harmony export */   PartCtrlSeq: () => (/* binding */ PartCtrlSeq),
/* harmony export */   RefArgument: () => (/* binding */ RefArgument),
/* harmony export */   RefCtrlSeq: () => (/* binding */ RefCtrlSeq),
/* harmony export */   RefStarrableCtrlSeq: () => (/* binding */ RefStarrableCtrlSeq),
/* harmony export */   RenewCommandCtrlSeq: () => (/* binding */ RenewCommandCtrlSeq),
/* harmony export */   RenewEnvironmentCtrlSeq: () => (/* binding */ RenewEnvironmentCtrlSeq),
/* harmony export */   RightCtrlSeq: () => (/* binding */ RightCtrlSeq),
/* harmony export */   Section: () => (/* binding */ Section),
/* harmony export */   SectionCtrlSeq: () => (/* binding */ SectionCtrlSeq),
/* harmony export */   SectioningArgument: () => (/* binding */ SectioningArgument),
/* harmony export */   SetLengthCtrlSeq: () => (/* binding */ SetLengthCtrlSeq),
/* harmony export */   ShortArg: () => (/* binding */ ShortArg),
/* harmony export */   ShortOptionalArg: () => (/* binding */ ShortOptionalArg),
/* harmony export */   ShortTextArgument: () => (/* binding */ ShortTextArgument),
/* harmony export */   SpaceDelimitedLiteralArgContent: () => (/* binding */ SpaceDelimitedLiteralArgContent),
/* harmony export */   SubParagraph: () => (/* binding */ SubParagraph),
/* harmony export */   SubParagraphCtrlSeq: () => (/* binding */ SubParagraphCtrlSeq),
/* harmony export */   SubSection: () => (/* binding */ SubSection),
/* harmony export */   SubSectionCtrlSeq: () => (/* binding */ SubSectionCtrlSeq),
/* harmony export */   SubSubSection: () => (/* binding */ SubSubSection),
/* harmony export */   SubSubSectionCtrlSeq: () => (/* binding */ SubSubSectionCtrlSeq),
/* harmony export */   SubfileCtrlSeq: () => (/* binding */ SubfileCtrlSeq),
/* harmony export */   TableEnvName: () => (/* binding */ TableEnvName),
/* harmony export */   TableEnvironment: () => (/* binding */ TableEnvironment),
/* harmony export */   TabularArgument: () => (/* binding */ TabularArgument),
/* harmony export */   TabularContent: () => (/* binding */ TabularContent),
/* harmony export */   TabularEnvName: () => (/* binding */ TabularEnvName),
/* harmony export */   TabularEnvironment: () => (/* binding */ TabularEnvironment),
/* harmony export */   Text: () => (/* binding */ Text),
/* harmony export */   TextArgument: () => (/* binding */ TextArgument),
/* harmony export */   TextBoldCtrlSeq: () => (/* binding */ TextBoldCtrlSeq),
/* harmony export */   TextColorCtrlSeq: () => (/* binding */ TextColorCtrlSeq),
/* harmony export */   TextItalicCtrlSeq: () => (/* binding */ TextItalicCtrlSeq),
/* harmony export */   TextMediumCtrlSeq: () => (/* binding */ TextMediumCtrlSeq),
/* harmony export */   TextSansSerifCtrlSeq: () => (/* binding */ TextSansSerifCtrlSeq),
/* harmony export */   TextSmallCapsCtrlSeq: () => (/* binding */ TextSmallCapsCtrlSeq),
/* harmony export */   TextStrikeOutCtrlSeq: () => (/* binding */ TextStrikeOutCtrlSeq),
/* harmony export */   TextSubscriptCtrlSeq: () => (/* binding */ TextSubscriptCtrlSeq),
/* harmony export */   TextSuperscriptCtrlSeq: () => (/* binding */ TextSuperscriptCtrlSeq),
/* harmony export */   TextTeletypeCtrlSeq: () => (/* binding */ TextTeletypeCtrlSeq),
/* harmony export */   TheoremStyleCtrlSeq: () => (/* binding */ TheoremStyleCtrlSeq),
/* harmony export */   TikzPictureContent: () => (/* binding */ TikzPictureContent),
/* harmony export */   TikzPictureEnvName: () => (/* binding */ TikzPictureEnvName),
/* harmony export */   TikzPictureEnvironment: () => (/* binding */ TikzPictureEnvironment),
/* harmony export */   Tilde: () => (/* binding */ Tilde),
/* harmony export */   TitleCtrlSeq: () => (/* binding */ TitleCtrlSeq),
/* harmony export */   TopRuleCtrlSeq: () => (/* binding */ TopRuleCtrlSeq),
/* harmony export */   TrailingContent: () => (/* binding */ TrailingContent),
/* harmony export */   TrailingWhitespaceOnly: () => (/* binding */ TrailingWhitespaceOnly),
/* harmony export */   UnderlineCtrlSeq: () => (/* binding */ UnderlineCtrlSeq),
/* harmony export */   UnknownCommand: () => (/* binding */ UnknownCommand),
/* harmony export */   UrlArgument: () => (/* binding */ UrlArgument),
/* harmony export */   UrlCtrlSeq: () => (/* binding */ UrlCtrlSeq),
/* harmony export */   UsePackageCtrlSeq: () => (/* binding */ UsePackageCtrlSeq),
/* harmony export */   VerbContent: () => (/* binding */ VerbContent),
/* harmony export */   VerbCtrlSeq: () => (/* binding */ VerbCtrlSeq),
/* harmony export */   VerbatimContent: () => (/* binding */ VerbatimContent),
/* harmony export */   VerbatimEnvName: () => (/* binding */ VerbatimEnvName),
/* harmony export */   VerbatimEnvironment: () => (/* binding */ VerbatimEnvironment),
/* harmony export */   Whitespace: () => (/* binding */ Whitespace),
/* harmony export */   endOfArguments: () => (/* binding */ endOfArguments),
/* harmony export */   endOfArgumentsAndOptionals: () => (/* binding */ endOfArgumentsAndOptionals),
/* harmony export */   hasMoreArguments: () => (/* binding */ hasMoreArguments),
/* harmony export */   hasMoreArgumentsOrOptionals: () => (/* binding */ hasMoreArgumentsOrOptionals)
/* harmony export */ });
// This file was generated by lezer-generator. You probably shouldn't edit it.
const
  VerbContent = 1,
  LstInlineContent = 2,
  LiteralArgContent = 3,
  SpaceDelimitedLiteralArgContent = 4,
  VerbatimContent = 5,
  Csname = 6,
  TrailingWhitespaceOnly = 7,
  TrailingContent = 8,
  hasMoreArguments = 392,
  endOfArguments = 393,
  hasMoreArgumentsOrOptionals = 394,
  endOfArgumentsAndOptionals = 395,
  Begin = 9,
  End = 10,
  RefCtrlSeq = 11,
  RefStarrableCtrlSeq = 12,
  CiteCtrlSeq = 13,
  CiteStarrableCtrlSeq = 14,
  LabelCtrlSeq = 15,
  MathTextCtrlSeq = 16,
  HboxCtrlSeq = 17,
  TitleCtrlSeq = 18,
  DocumentClassCtrlSeq = 19,
  UsePackageCtrlSeq = 20,
  HrefCtrlSeq = 21,
  UrlCtrlSeq = 22,
  VerbCtrlSeq = 23,
  LstInlineCtrlSeq = 24,
  IncludeGraphicsCtrlSeq = 25,
  IncludeSvgCtrlSeq = 26,
  CaptionCtrlSeq = 27,
  DefCtrlSeq = 28,
  LetCtrlSeq = 29,
  LeftCtrlSeq = 30,
  RightCtrlSeq = 31,
  NewCommandCtrlSeq = 32,
  RenewCommandCtrlSeq = 33,
  NewEnvironmentCtrlSeq = 34,
  RenewEnvironmentCtrlSeq = 35,
  BookCtrlSeq = 36,
  PartCtrlSeq = 37,
  ChapterCtrlSeq = 38,
  SectionCtrlSeq = 39,
  SubSectionCtrlSeq = 40,
  SubSubSectionCtrlSeq = 41,
  ParagraphCtrlSeq = 42,
  SubParagraphCtrlSeq = 43,
  InputCtrlSeq = 44,
  IncludeCtrlSeq = 45,
  SubfileCtrlSeq = 46,
  ItemCtrlSeq = 47,
  NewTheoremCtrlSeq = 48,
  TheoremStyleCtrlSeq = 49,
  CenteringCtrlSeq = 50,
  BibliographyCtrlSeq = 51,
  BibliographyStyleCtrlSeq = 52,
  AuthorCtrlSeq = 53,
  AffilCtrlSeq = 54,
  AffiliationCtrlSeq = 55,
  DateCtrlSeq = 56,
  MaketitleCtrlSeq = 57,
  TextColorCtrlSeq = 58,
  ColorBoxCtrlSeq = 59,
  HLineCtrlSeq = 60,
  TopRuleCtrlSeq = 61,
  MidRuleCtrlSeq = 62,
  BottomRuleCtrlSeq = 63,
  MultiColumnCtrlSeq = 64,
  ParBoxCtrlSeq = 65,
  TextBoldCtrlSeq = 66,
  TextItalicCtrlSeq = 67,
  TextSmallCapsCtrlSeq = 68,
  TextTeletypeCtrlSeq = 69,
  TextMediumCtrlSeq = 70,
  TextSansSerifCtrlSeq = 71,
  TextSuperscriptCtrlSeq = 72,
  TextSubscriptCtrlSeq = 73,
  TextStrikeOutCtrlSeq = 74,
  EmphasisCtrlSeq = 75,
  UnderlineCtrlSeq = 76,
  SetLengthCtrlSeq = 77,
  FootnoteCtrlSeq = 78,
  EndnoteCtrlSeq = 79,
  DocumentEnvName = 80,
  TabularEnvName = 81,
  EquationEnvName = 82,
  EquationArrayEnvName = 83,
  VerbatimEnvName = 84,
  TikzPictureEnvName = 85,
  FigureEnvName = 86,
  ListEnvName = 87,
  TableEnvName = 88,
  OpenParenCtrlSym = 89,
  CloseParenCtrlSym = 90,
  OpenBracketCtrlSym = 91,
  CloseBracketCtrlSym = 92,
  LineBreakCtrlSym = 93,
  Comment = 94,
  LaTeX = 95,
  Text = 96,
  BlankLine = 97,
  KnownEnvironment = 98,
  DocumentEnvironment = 99,
  OpenBrace = 102,
  CloseBrace = 103,
  OptionalArgument = 104,
  OpenBracket = 105,
  ShortOptionalArg = 106,
  Command = 107,
  Whitespace = 110,
  TextArgument = 111,
  LongArg = 112,
  CloseBracket = 113,
  Environment = 115,
  EnvName = 118,
  ShortTextArgument = 125,
  ShortArg = 126,
  PackageArgument = 135,
  UrlArgument = 139,
  FilePathArgument = 147,
  LabelArgument = 152,
  RefArgument = 154,
  BibKeyArgument = 156,
  CtrlSym = 158,
  MacroParameter = 159,
  OptionalMacroParameter = 160,
  DefinitionArgument = 161,
  NewLine = 162,
  DefinitionFragment = 163,
  DefinitionFragmentCommand = 164,
  CtrlSeq = 166,
  DefinitionFragmentArgument = 167,
  KnownCtrlSym = 168,
  Dollar = 171,
  Normal = 172,
  Ampersand = 173,
  Tilde = 174,
  SectioningArgument = 176,
  BareFilePathArgument = 185,
  TabularArgument = 197,
  TabularContent = 198,
  UnknownCommand = 215,
  DollarMath = 216,
  InlineMath = 217,
  Math = 218,
  MathCommand = 219,
  MathArgument = 234,
  MathDelimitedGroup = 288,
  MathOpening = 289,
  MathDelimiter = 290,
  MathClosing = 291,
  MathSpecialChar = 292,
  Number = 293,
  MathChar = 294,
  DisplayMath = 295,
  BracketMath = 296,
  OpenBracketMath = 297,
  CloseBracketMath = 298,
  ParenMath = 299,
  OpenParenMath = 300,
  CloseParenMath = 301,
  TabularEnvironment = 304,
  EquationEnvironment = 309,
  EquationArrayEnvironment = 314,
  VerbatimEnvironment = 318,
  TikzPictureEnvironment = 323,
  TikzPictureContent = 327,
  FigureEnvironment = 330,
  ListEnvironment = 334,
  TableEnvironment = 338,
  Book = 343,
  Part = 346,
  Chapter = 349,
  Section = 352,
  SubSection = 355,
  SubSubSection = 358,
  Paragraph = 361,
  SubParagraph = 364


/***/ },

/***/ "../../frontend/js/features/source-editor/lezer-latex/tokens.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   argumentListTokenizer: () => (/* binding */ argumentListTokenizer),
/* harmony export */   argumentListWithOptionalTokenizer: () => (/* binding */ argumentListWithOptionalTokenizer),
/* harmony export */   csnameTokenizer: () => (/* binding */ csnameTokenizer),
/* harmony export */   elementContext: () => (/* binding */ elementContext),
/* harmony export */   literalArgTokenizer: () => (/* binding */ literalArgTokenizer),
/* harmony export */   lstinlineTokenizer: () => (/* binding */ lstinlineTokenizer),
/* harmony export */   spaceDelimitedLiteralArgTokenizer: () => (/* binding */ spaceDelimitedLiteralArgTokenizer),
/* harmony export */   specializeCtrlSeq: () => (/* binding */ specializeCtrlSeq),
/* harmony export */   specializeCtrlSym: () => (/* binding */ specializeCtrlSym),
/* harmony export */   specializeEnvName: () => (/* binding */ specializeEnvName),
/* harmony export */   trailingContentTokenizer: () => (/* binding */ trailingContentTokenizer),
/* harmony export */   verbTokenizer: () => (/* binding */ verbTokenizer),
/* harmony export */   verbatimTokenizer: () => (/* binding */ verbatimTokenizer)
/* harmony export */ });
/* harmony import */ var _lezer_lr__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("../../.yarn/cache/@lezer-lr-npm-1.4.7-c15665133d-543c2e1aae.zip/node_modules/@lezer/lr/dist/index.js");
/* harmony import */ var _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__("../../frontend/js/features/source-editor/lezer-latex/latex.terms.mjs");
/* Hand-written tokenizer for LaTeX. */





const MAX_ARGUMENT_LOOKAHEAD = 100

function nameChar(ch) {
  // we accept A-Z a-z 0-9 * + @ in environment names
  return (
    (ch >= 65 && ch <= 90) ||
    (ch >= 97 && ch <= 122) ||
    (ch >= 48 && ch <= 57) ||
    ch === 42 ||
    ch === 43 ||
    ch === 64
  )
}

// match [a-zA-Z]
function alphaChar(ch) {
  return (ch >= 65 && ch <= 90) || (ch >= 97 && ch <= 122)
}

let cachedName = null
let cachedInput = null
let cachedPos = 0
function envNameAfter(input, offset) {
  const pos = input.pos + offset
  if (cachedInput === input && cachedPos === pos) {
    return cachedName
  }
  if (input.peek(offset) !== '{'.charCodeAt(0)) return
  offset++
  let name = ''
  for (;;) {
    const next = input.peek(offset)
    if (!nameChar(next)) break
    name += String.fromCharCode(next)
    offset++
  }
  cachedInput = input
  cachedPos = pos
  return (cachedName = name || null)
}

function ElementContext(name, parent) {
  this.name = name
  this.parent = parent
  this.hash = parent ? parent.hash : 0
  for (let i = 0; i < name.length; i++)
    this.hash +=
      (this.hash << 4) + name.charCodeAt(i) + (name.charCodeAt(i) << 8)
}

const elementContext = new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.ContextTracker({
  start: null,
  shift(context, term, stack, input) {
    return term === _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.Begin
      ? new ElementContext(envNameAfter(input, '\\begin'.length) || '', context)
      : context
  },
  reduce(context, term) {
    return term === _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.KnownEnvironment && context ? context.parent : context
  },
  reuse(context, node, _stack, input) {
    const type = node.type.id
    return type === _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.Begin
      ? new ElementContext(envNameAfter(input, 0) || '', context)
      : context
  },
  hash(context) {
    return context ? context.hash : 0
  },
  strict: false,
})

// tokenizer for \verb|...| commands
const verbTokenizer = new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.ExternalTokenizer(
  input => {
    if (input.next === '*'.charCodeAt(0)) input.advance()
    const delimiter = input.next
    if (delimiter === -1) return // hit end of file
    if (/\s|\*/.test(String.fromCharCode(delimiter))) return // invalid delimiter
    input.advance()
    for (;;) {
      const next = input.next
      if (next === -1 || next === CHAR_NEWLINE) return
      input.advance()
      if (next === delimiter) break
    }
    return input.acceptToken(_latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.VerbContent)
  },
  { contextual: false }
)

// tokenizer for \lstinline|...| commands
const lstinlineTokenizer = new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.ExternalTokenizer(
  input => {
    let delimiter = input.next
    if (delimiter === -1) return // hit end of file
    if (/\s/.test(String.fromCharCode(delimiter))) {
      return // invalid delimiter
    }
    if (delimiter === CHAR_OPEN_BRACE) {
      delimiter = CHAR_CLOSE_BRACE
    }
    input.advance()
    for (;;) {
      const next = input.next
      if (next === -1 || next === CHAR_NEWLINE) return
      input.advance()
      if (next === delimiter) break
    }
    return input.acceptToken(_latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.LstInlineContent)
  },
  { contextual: false }
)

const matchForward = (input, expected, offset = 0) => {
  for (let i = 0; i < expected.length; i++) {
    if (String.fromCharCode(input.peek(offset + i)) !== expected[i]) {
      return false
    }
  }
  return true
}

// tokenizer for \begin{verbatim}...\end{verbatim} environments
const verbatimTokenizer = new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.ExternalTokenizer(
  (input, stack) => {
    const delimiter = '\\end{' + stack.context.name + '}'
    for (let offset = 0; ; offset++) {
      const next = input.peek(offset)
      if (next === -1 || matchForward(input, delimiter, offset)) {
        return input.acceptToken(_latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.VerbatimContent, offset)
      }
    }
  },
  { contextual: false }
)

// tokenizer for \href{...} and similar commands
const literalArgTokenizer = new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.ExternalTokenizer(
  input => {
    for (let offset = 0; ; offset++) {
      const next = input.peek(offset)
      if (next === -1 || next === CHAR_CLOSE_BRACE) {
        return input.acceptToken(_latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.LiteralArgContent, offset)
      }
    }
  },
  { contextual: false }
)

// tokenizer for literal content delimited by whitespace, such as in `\input foo.tex`
const spaceDelimitedLiteralArgTokenizer = new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.ExternalTokenizer(
  input => {
    for (let offset = 0; ; offset++) {
      const next = input.peek(offset)
      if (next === -1 || next === CHAR_SPACE || next === CHAR_NEWLINE) {
        return input.acceptToken(_latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.SpaceDelimitedLiteralArgContent, offset)
      }
    }
  },
  { contextual: false }
)

// helper function to look up charCodes
function _char(s) {
  return s.charCodeAt(0)
}

const CHAR_BACKSLASH = _char('\\')
const CHAR_OPEN_BRACE = _char('{')
const CHAR_OPEN_BRACKET = _char('[')
const CHAR_CLOSE_BRACE = _char('}')
const CHAR_TAB = _char('\t')
const CHAR_SPACE = _char(' ')
const CHAR_NEWLINE = _char('\n')

const lookaheadTokenizer = getToken =>
  new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.ExternalTokenizer(
    input => {
      for (let i = 0; i < MAX_ARGUMENT_LOOKAHEAD; ++i) {
        const next = input.peek(i)
        if (next === CHAR_SPACE || next === CHAR_TAB) {
          continue
        }
        const token = getToken(next)
        if (token) {
          input.acceptToken(token)
          return
        }
      }
    },
    { contextual: false, fallback: true }
  )

const argumentListTokenizer = lookaheadTokenizer(next => {
  if (next === CHAR_OPEN_BRACE) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.hasMoreArguments
  } else {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.endOfArguments
  }
})

const argumentListWithOptionalTokenizer = lookaheadTokenizer(next => {
  if (next === CHAR_OPEN_BRACE || next === CHAR_OPEN_BRACKET) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.hasMoreArgumentsOrOptionals
  } else {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.endOfArgumentsAndOptionals
  }
})

const CHAR_AT_SYMBOL = _char('@')

const csnameTokenizer = new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.ExternalTokenizer(input => {
  let offset = 0
  let end = -1
  // look at the first character, we are looking for acceptable control sequence names
  // including @ signs, \\[a-zA-Z@]+
  const next = input.peek(offset)
  if (next === -1) {
    return
  }
  // reject anything not starting with a backslash,
  // we only accept control sequences
  if (next !== CHAR_BACKSLASH) {
    return
  }
  offset++
  for (;;) {
    const next = input.peek(offset)
    // stop when we reach the end of file or a non-csname character
    if (next === -1 || !(alphaChar(next) || next === CHAR_AT_SYMBOL)) {
      end = offset - 1
      break
    }
    end = offset
    offset++
  }
  if (end === -1) return
  // accept the content as a valid control sequence
  return input.acceptToken(_latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.Csname, end + 1)
})

const END_DOCUMENT_MARK = '\\end{document}'.split('').reverse()

const trailingContentTokenizer = new _lezer_lr__WEBPACK_IMPORTED_MODULE_0__.ExternalTokenizer(input => {
  if (input.next === -1) return // no trailing content
  // Look back for end-document mark, bail out if any characters do not match
  for (let i = 1; i < END_DOCUMENT_MARK.length + 1; i++) {
    if (String.fromCharCode(input.peek(-i)) !== END_DOCUMENT_MARK[i - 1]) {
      return
    }
  }
  while (input.next === CHAR_SPACE || input.next === CHAR_NEWLINE) {
    const next = input.advance()
    if (next === -1) return input.acceptToken(_latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TrailingWhitespaceOnly) // trailing whitespace only
  }
  // accept the all content up to the end of the document
  while (input.advance() !== -1) {
    //
  }
  return input.acceptToken(_latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TrailingContent)
})

const refCommands = new Set([
  '\\fullref',
  '\\Vref',
  '\\autopageref',
  '\\autoref',
  '\\eqref',
  '\\labelcpageref',
  '\\labelcref',
  '\\lcnamecref',
  '\\lcnamecrefs',
  '\\namecref',
  '\\nameCref',
  '\\namecrefs',
  '\\nameCrefs',
  '\\thnameref',
  '\\thref',
  '\\titleref',
  '\\vrefrange',
  '\\Crefrange',
  '\\Crefrang',
  '\\fref',
  '\\pref',
  '\\tref',
  '\\Aref',
  '\\Bref',
  '\\Pref',
  '\\Sref',
  '\\vref',
  '\\nameref',
])

const refStarrableCommands = new Set([
  '\\vpageref',
  '\\vref',
  '\\zcpageref',
  '\\zcref',
  '\\zfullref',
  '\\zref',
  '\\zvpageref',
  '\\zvref',
  '\\cref',
  '\\Cref',
  '\\pageref',
  '\\ref',
  '\\Ref',
  '\\subref',
  '\\zpageref',
  '\\ztitleref',
  '\\vpagerefrange',
  '\\zvpagerefrange',
  '\\zvrefrange',
  '\\crefrange',
])

const citeCommands = new Set([
  '\\autocites',
  '\\Autocites',
  '\\Cite',
  '\\citeA',
  '\\citealp',
  '\\Citealp',
  '\\citealt',
  '\\Citealt',
  '\\citeauthorNP',
  '\\citeauthorp',
  '\\Citeauthorp',
  '\\citeauthort',
  '\\Citeauthort',
  '\\citeNP',
  '\\citenum',
  '\\citen',
  '\\citeonline',
  '\\cites',
  '\\Cites',
  '\\citeurl',
  '\\citeyearpar',
  '\\defcitealias',
  '\\fnotecite',
  '\\footcite',
  '\\footcitetext',
  '\\footfullcite',
  '\\footnotecites',
  '\\Footnotecites',
  '\\fullcite',
  '\\fullciteA',
  '\\fullciteauthor',
  '\\fullciteauthorNP',
  '\\maskcite',
  '\\maskciteA',
  '\\maskcitealp',
  '\\maskCitealp',
  '\\maskcitealt',
  '\\maskCitealt',
  '\\maskciteauthor',
  '\\maskciteauthorNP',
  '\\maskciteauthorp',
  '\\maskCiteauthorp',
  '\\maskciteauthort',
  '\\maskCiteauthort',
  '\\maskciteNP',
  '\\maskcitenum',
  '\\maskcitep',
  '\\maskCitep',
  '\\maskcitepalias',
  '\\maskcitet',
  '\\maskCitet',
  '\\maskcitetalias',
  '\\maskciteyear',
  '\\maskciteyearNP',
  '\\maskciteyearpar',
  '\\maskfullcite',
  '\\maskfullciteA',
  '\\maskfullciteauthor',
  '\\maskfullciteauthorNP',
  '\\masknocite',
  '\\maskshortcite',
  '\\maskshortciteA',
  '\\maskshortciteauthor',
  '\\maskshortciteauthorNP',
  '\\maskshortciteNP',
  '\\mautocite',
  '\\Mautocite',
  '\\mcite',
  '\\Mcite',
  '\\mfootcite',
  '\\mfootcitetext',
  '\\mparencite',
  '\\Mparencite',
  '\\msupercite',
  '\\mtextcite',
  '\\Mtextcite',
  '\\nocite',
  '\\nocitemeta',
  '\\notecite',
  '\\Parencite',
  '\\parencites',
  '\\Parencites',
  '\\pnotecite',
  '\\shortcite',
  '\\shortciteA',
  '\\shortciteauthor',
  '\\shortciteauthorNP',
  '\\shortciteNP',
  '\\smartcite',
  '\\Smartcite',
  '\\smartcites',
  '\\Smartcites',
  '\\supercite',
  '\\supercites',
  '\\textcite',
  '\\Textcite',
  '\\textcites',
  '\\Textcites',
])

const citeStarredCommands = new Set([
  '\\cite',
  '\\citeauthor',
  '\\Citeauthor',
  '\\citedate',
  '\\citep',
  '\\citepalias',
  '\\Citep',
  '\\citetitle',
  '\\citeyear',
  '\\parencite',
  '\\citet',
  '\\citetalias',
  '\\autocite',
  '\\Autocite',
])

const labelCommands = new Set(['\\label', '\\thlabel', '\\zlabel'])

const mathTextCommands = new Set(['\\text', '\\tag', '\\textrm', '\\intertext'])

const otherKnowncommands = {
  '\\hbox': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.HboxCtrlSeq,
  '\\title': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TitleCtrlSeq,
  '\\author': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.AuthorCtrlSeq,
  '\\affil': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.AffilCtrlSeq,
  '\\affiliation': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.AffiliationCtrlSeq,
  '\\date': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.DateCtrlSeq,
  '\\documentclass': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.DocumentClassCtrlSeq,
  '\\usepackage': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.UsePackageCtrlSeq,
  '\\href': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.HrefCtrlSeq,
  '\\url': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.UrlCtrlSeq,
  '\\verb': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.VerbCtrlSeq,
  '\\lstinline': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.LstInlineCtrlSeq,
  '\\includegraphics': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.IncludeGraphicsCtrlSeq,
  '\\includesvg': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.IncludeSvgCtrlSeq,
  '\\caption': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.CaptionCtrlSeq,
  '\\def': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.DefCtrlSeq,
  '\\let': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.LetCtrlSeq,
  '\\left': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.LeftCtrlSeq,
  '\\right': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.RightCtrlSeq,
  '\\newcommand': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.NewCommandCtrlSeq,
  '\\renewcommand': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.RenewCommandCtrlSeq,
  '\\newenvironment': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.NewEnvironmentCtrlSeq,
  '\\renewenvironment': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.RenewEnvironmentCtrlSeq,
  '\\book': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.BookCtrlSeq,
  '\\part': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.PartCtrlSeq,
  '\\addpart': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.PartCtrlSeq,
  '\\chapter': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.ChapterCtrlSeq,
  '\\addchap': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.ChapterCtrlSeq,
  '\\section': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.SectionCtrlSeq,
  '\\addseq': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.SectionCtrlSeq,
  '\\subsection': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.SubSectionCtrlSeq,
  '\\subsubsection': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.SubSubSectionCtrlSeq,
  '\\paragraph': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.ParagraphCtrlSeq,
  '\\subparagraph': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.SubParagraphCtrlSeq,
  '\\input': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.InputCtrlSeq,
  '\\include': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.IncludeCtrlSeq,
  '\\subfile': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.SubfileCtrlSeq,
  '\\item': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.ItemCtrlSeq,
  '\\centering': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.CenteringCtrlSeq,
  '\\newtheorem': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.NewTheoremCtrlSeq,
  '\\theoremstyle': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TheoremStyleCtrlSeq,
  '\\bibliography': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.BibliographyCtrlSeq,
  '\\bibliographystyle': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.BibliographyStyleCtrlSeq,
  '\\maketitle': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.MaketitleCtrlSeq,
  '\\textcolor': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextColorCtrlSeq,
  '\\colorbox': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.ColorBoxCtrlSeq,
  '\\hline': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.HLineCtrlSeq,
  '\\toprule': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TopRuleCtrlSeq,
  '\\midrule': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.MidRuleCtrlSeq,
  '\\bottomrule': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.BottomRuleCtrlSeq,
  '\\multicolumn': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.MultiColumnCtrlSeq,
  '\\parbox': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.ParBoxCtrlSeq,
  '\\textbf': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextBoldCtrlSeq,
  '\\textit': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextItalicCtrlSeq,
  '\\textsc': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextSmallCapsCtrlSeq,
  '\\texttt': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextTeletypeCtrlSeq,
  '\\textmd': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextMediumCtrlSeq,
  '\\textsf': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextSansSerifCtrlSeq,
  '\\textsuperscript': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextSuperscriptCtrlSeq,
  '\\textsubscript': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextSubscriptCtrlSeq,
  '\\sout': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TextStrikeOutCtrlSeq,
  '\\emph': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.EmphasisCtrlSeq,
  '\\underline': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.UnderlineCtrlSeq,
  '\\setlength': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.SetLengthCtrlSeq,
  '\\footnote': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.FootnoteCtrlSeq,
  '\\endnote': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.EndnoteCtrlSeq,
}
// specializer for control sequences
// return new tokens for specific control sequences
const specializeCtrlSeq = name => {
  if (name === '\\begin') return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.Begin
  if (name === '\\end') return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.End
  if (refCommands.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.RefCtrlSeq
  }
  if (refStarrableCommands.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.RefStarrableCtrlSeq
  }
  if (citeCommands.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.CiteCtrlSeq
  }
  if (citeStarredCommands.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.CiteStarrableCtrlSeq
  }
  if (labelCommands.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.LabelCtrlSeq
  }
  if (mathTextCommands.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.MathTextCtrlSeq
  }
  return otherKnowncommands[name] || -1
}

const tabularEnvNames = new Set([
  'tabular',
  'xltabular',
  'tabularx',
  'longtable',
])

const equationEnvNames = new Set([
  'equation',
  'equation*',
  'displaymath',
  'displaymath*',
  'math',
  'math*',
  'multline',
  'multline*',
  'matrix',
  'tikzcd',
])

const equationArrayEnvNames = new Set([
  'array',
  'eqnarray',
  'eqnarray*',
  'align',
  'align*',
  'alignat',
  'alignat*',
  'flalign',
  'flalign*',
  'gather',
  'gather*',
  'pmatrix',
  'pmatrix*',
  'bmatrix',
  'bmatrix*',
  'Bmatrix',
  'Bmatrix*',
  'vmatrix',
  'vmatrix*',
  'Vmatrix',
  'Vmatrix*',
  'smallmatrix',
  'smallmatrix*',
  'split',
  'split*',
  'gathered',
  'gathered*',
  'aligned',
  'aligned*',
  'alignedat',
  'alignedat*',
  'cases',
  'cases*',
  'dcases',
  'dcases*',
  'rcases',
  'rcases*',
  'IEEEeqnarray',
  'IEEEeqnarray*',
  'subeqnarray',
  'subeqnarray*',
])

const verbatimEnvNames = new Set([
  'verbatim',
  'boxedverbatim',
  'lstlisting',
  'minted',
  'Verbatim',
  'lstlisting',
  'tcblisting',
  'codeexample',
  'comment',
])

const otherKnownEnvNames = {
  document: _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.DocumentEnvName,
  tikzpicture: _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TikzPictureEnvName,
  figure: _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.FigureEnvName,
  'figure*': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.FigureEnvName,
  subfigure: _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.FigureEnvName,
  enumerate: _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.ListEnvName,
  itemize: _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.ListEnvName,
  table: _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TableEnvName,
  description: _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.ListEnvName,
}

const specializeEnvName = name => {
  if (tabularEnvNames.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.TabularEnvName
  }
  if (equationEnvNames.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.EquationEnvName
  }
  if (equationArrayEnvNames.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.EquationArrayEnvName
  }
  if (verbatimEnvNames.has(name)) {
    return _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.VerbatimEnvName
  }
  return otherKnownEnvNames[name] || -1
}

const otherKnownCtrlSyms = {
  '\\(': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.OpenParenCtrlSym,
  '\\)': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.CloseParenCtrlSym,
  '\\[': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.OpenBracketCtrlSym,
  '\\]': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.CloseBracketCtrlSym,
  '\\\\': _latex_terms_mjs__WEBPACK_IMPORTED_MODULE_1__.LineBreakCtrlSym,
}

const specializeCtrlSym = name => {
  return otherKnownCtrlSyms[name] || -1
}


/***/ },

/***/ "./modules/project-inspection/app/src/analyzer/analyze-project.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   analyzeProject: () => (/* binding */ analyzeProject),
/* harmony export */   "default": () => (__WEBPACK_DEFAULT_EXPORT__)
/* harmony export */ });
/* harmony import */ var node_path__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("node:path");
/* harmony import */ var _latex_extractor_mjs__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/latex-extractor.mjs");
/* harmony import */ var _bibtex_extractor_mjs__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/bibtex-extractor.mjs");
/* harmony import */ var _resource_resolver_mjs__WEBPACK_IMPORTED_MODULE_3__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/resource-resolver.mjs");
/* harmony import */ var _cycles_mjs__WEBPACK_IMPORTED_MODULE_4__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/cycles.mjs");
/* harmony import */ var _graph_builder_mjs__WEBPACK_IMPORTED_MODULE_5__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/graph-builder.mjs");
/* harmony import */ var _issue_collector_mjs__WEBPACK_IMPORTED_MODULE_6__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/issue-collector.mjs");








const TEX_EXTENSIONS = new Set([
  '.tex',
  '.ltx',
  '.latex',
  '.rtex',
  '.rnw',
  '.sty',
  '.cls',
  '.def',
  '.clo',
  '.dtx',
  '.ins',
  '.tikz',
  '.inc',
])
const IMAGE_EXTENSIONS = [
  '.pdf',
  '.png',
  '.jpg',
  '.jpeg',
  '.eps',
  '.svg',
]
const BIB_EXTENSIONS = ['.bib', '.bibtex']
const PROJECT_CLASS_RELATIONS = new Set([
  'documentclass',
  'loadclass',
  'loadclasswithoptions',
])
const PATH_TARGET_ISSUE_TYPES = new Set([
  'missing-file',
  'missing-figure',
  'missing-bibliography',
  'unreferenced-figure',
  'possibly-unused-file',
])

const fileNodeId = filePath => `file:${filePath}`
const occurrenceNodeId = (kind, location, identity = '') =>
  `${kind}:${location.entityId}:${location.from}:${encodeURIComponent(identity)}`
const figureResourceNodeId = item =>
  occurrenceNodeId('figure-file', item.location, item.target)
const citationGroupNodeId = citation =>
  'citation-group:' +
  citation.location.entityId +
  ':' +
  encodeURIComponent(citation.key)

function normalizedBibliographyTitle(title) {
  const display = title
    .normalize('NFKC')
    .replace(/~/g, ' ')
    .replace(/[{}]/g, '')
    .replace(/\s+/gu, ' ')
    .trim()
  return { display, key: display.toLocaleLowerCase('en-US') }
}

function normalizedCaption(caption) {
  const key = caption
    .normalize('NFKC')
    .replace(/~/g, ' ')
    .replace(/[{}]/g, '')
    .replace(/\s+/gu, ' ')
    .trim()
    .toLocaleLowerCase('en-US')
  return key
}

function bibliographyEntrySignature(entries) {
  return entries
    .map(
      entry =>
        entry.location.entityId +
        ':' +
        entry.location.from +
        ':' +
        entry.location.to
    )
    .sort()
    .join(',')
}

function componentIssueKey(type, target) {
  let identity = String(target ?? '')
  if (PATH_TARGET_ISSUE_TYPES.has(type)) {
    identity = node_path__WEBPACK_IMPORTED_MODULE_0__.posix.normalize(identity.replace(/^\.\//, ''))
  }
  return `${type}:${encodeURIComponent(identity)}`
}

function collectBibliographyEntry(entriesByKey, entriesByTitle, entry) {
  const keyEntries = entriesByKey.get(entry.key) ?? []
  keyEntries.push(entry)
  entriesByKey.set(entry.key, keyEntries)

  if (entry.title == null || !entry.titleLocation) return
  const normalizedTitle = normalizedBibliographyTitle(entry.title)
  if (!normalizedTitle.key) return
  const titleEntries = entriesByTitle.get(normalizedTitle.key) ?? []
  titleEntries.push({ ...entry, displayTitle: normalizedTitle.display })
  entriesByTitle.set(normalizedTitle.key, titleEntries)
}

function extension(filePath) {
  return node_path__WEBPACK_IMPORTED_MODULE_0__.posix.extname(filePath).toLowerCase()
}

function isTexLike(filePath) {
  return TEX_EXTENSIONS.has(extension(filePath))
}

function isBibliography(filePath) {
  return BIB_EXTENSIONS.includes(extension(filePath))
}

function relationNode(graph, kind, item, parentPath, label, identity = label) {
  const id = occurrenceNodeId(kind, item.location, identity)
  graph.addNode({
    id,
    kind,
    label,
    path: parentPath,
    location: item.location,
    parentId: fileNodeId(parentPath),
  })
  graph.addEdge({
    kind: 'contains',
    from: fileNodeId(parentPath),
    to: id,
  })
  return id
}

function environmentNodeId(environment) {
  return occurrenceNodeId(
    environment.kind,
    environment.location,
    environment.name
  )
}

function environmentLabel(environment, sourcePath) {
  const prefix = environment.kind === 'figure' ? 'Figure' : 'Table'
  return (
    environment.labels[0] ??
    `${prefix} ${sourcePath}:${environment.location.line}`
  )
}

function resolveRelations(parsed, availablePaths) {
  const result = {
    includes: [],
    figures: [],
    bibliographies: [],
  }
  for (const item of parsed.includes) {
    const resolve = extensions =>
      (0,_resource_resolver_mjs__WEBPACK_IMPORTED_MODULE_3__.resolveProjectPath)({
        target: item.target,
        sourcePath: parsed.path,
        availablePaths,
        extensions,
      })
    let resolution = resolve(
      PROJECT_CLASS_RELATIONS.has(item.relation) ? ['.cls'] : ['.tex']
    )
    if (item.relation === 'input' && resolution.status === 'missing') {
      resolution = resolve(
        [...TEX_EXTENSIONS].filter(extension => extension !== '.tex')
      )
    }
    if (
      PROJECT_CLASS_RELATIONS.has(item.relation) &&
      resolution.status === 'missing'
    ) {
      continue
    }
    result.includes.push({
      ...item,
      resolution,
    })
  }
  for (const item of parsed.figures) {
    result.figures.push({
      ...item,
      resolution: (0,_resource_resolver_mjs__WEBPACK_IMPORTED_MODULE_3__.resolveProjectPath)({
        target: item.target,
        sourcePath: parsed.path,
        availablePaths,
        extensions: IMAGE_EXTENSIONS,
        additionalRoots: parsed.graphicPaths,
      }),
    })
  }
  for (const item of parsed.bibliographyFiles) {
    result.bibliographies.push({
      ...item,
      resolution: (0,_resource_resolver_mjs__WEBPACK_IMPORTED_MODULE_3__.resolveProjectPath)({
        target: item.target,
        sourcePath: parsed.path,
        availablePaths,
        extensions: BIB_EXTENSIONS,
      }),
    })
  }
  return result
}

function buildCycleEdges(cycle, relationsByPath) {
  return cycle.path.slice(0, -1).flatMap((from, index) => {
    const to = cycle.path[index + 1]
    const relation = relationsByPath
      .get(from)
      ?.includes.find(
        item =>
          item.resolution.status === 'resolved' &&
          item.resolution.path === to
      )
    return relation ? [{ from, to, location: relation.location }] : []
  })
}

function addResolvedRelationToGraph(graph, sourcePath, kind, item) {
  const commandId = relationNode(
    graph,
    kind,
    item,
    sourcePath,
    item.target
  )
  if (item.resolution.status === 'resolved') {
    graph.addEdge({
      kind,
      from: commandId,
      to: fileNodeId(item.resolution.path),
    })
  } else if (item.resolution.status === 'missing') {
    const missingId = `missing:${kind}:${sourcePath}:${item.target}`
    graph.addNode({
      id: missingId,
      kind: 'missing-resource',
      label: item.target,
      status: 'missing',
      path: sourcePath,
    })
    graph.addEdge({ kind, from: commandId, to: missingId })
  }
  return commandId
}

function attachNodeToFile(graph, nodeId, filePath) {
  graph.addNode({ id: nodeId, parentId: fileNodeId(filePath) })
  graph.addEdge({
    kind: 'contains',
    from: fileNodeId(filePath),
    to: nodeId,
  })
}

function addFigureResourceToGraph(
  graph,
  sourcePath,
  item,
  environmentsByFrom,
  entityByPath
) {
  const id = figureResourceNodeId(item)
  const environment = environmentsByFrom.get(item.environmentFrom)
  const parentId = environment
    ? environmentNodeId(environment)
    : fileNodeId(sourcePath)
  const node = {
    id,
    kind: 'figure-file',
    label: item.target,
    parentId,
  }
  if (item.resolution.status === 'resolved') {
    node.path = item.resolution.path
    node.entityId = entityByPath.get(item.resolution.path)?.id
  } else {
    node.path = sourcePath
    node.location = item.location
  }
  graph.addNode(node)
  graph.addEdge({ kind: 'contains', from: parentId, to: id })
  return id
}

function collectScope(rootPath, latexByPath, relationsByPath, bibByPath) {
  const documents = new Set()
  const images = new Set()
  const bibliographies = new Set()
  const queue = [rootPath]
  let bibliographyIncomplete = false

  while (queue.length > 0) {
    const current = queue.shift()
    if (documents.has(current) || !latexByPath.has(current)) continue
    documents.add(current)
    const parsed = latexByPath.get(current)
    if (
      parsed.dynamicReferences.some(reference =>
        ['bibliography', 'addbibresource', 'bibliography-entry'].includes(
          reference.kind
        )
      )
    ) {
      bibliographyIncomplete = true
    }
    const relations = relationsByPath.get(current)
    for (const relation of relations.includes) {
      if (
        relation.resolution.status === 'resolved' &&
        latexByPath.has(relation.resolution.path)
      ) {
        queue.push(relation.resolution.path)
      }
    }
    for (const relation of relations.figures) {
      if (relation.resolution.status === 'resolved') {
        images.add(relation.resolution.path)
      } else if (relation.resolution.status === 'ambiguous') {
        for (const candidate of relation.resolution.candidates) {
          images.add(candidate)
        }
      }
    }
    for (const relation of relations.bibliographies) {
      if (relation.resolution.status === 'resolved') {
        bibliographies.add(relation.resolution.path)
        if (bibByPath.get(relation.resolution.path)?.unavailable) {
          bibliographyIncomplete = true
        }
      } else {
        bibliographyIncomplete = true
      }
    }
  }

  return { documents, images, bibliographies, bibliographyIncomplete }
}

function issueTitle(type, target) {
  const titles = {
    'missing-file': 'Missing included file',
    'missing-figure': 'Missing figure resource',
    'missing-bibliography': 'Missing bibliography file',
    'missing-reference': 'Reference has no matching label',
    'missing-citation': 'Citation has no matching bibliography entry',
    'duplicate-label': 'Duplicate label',
    'duplicate-figure-caption': 'Duplicate figure caption',
    'duplicate-table-caption': 'Duplicate table caption',
    'duplicate-bibliography-key': 'Duplicate bibliography key',
    'duplicate-bibliography-title': 'Duplicate bibliography title',
    'unreferenced-label': 'Label is not referenced',
    'unreferenced-figure': 'Figure is not referenced',
    'unreferenced-table': 'Table is not referenced',
    'unlabeled-figure': 'Figure has no label',
    'unlabeled-table': 'Table has no label',
    'unused-bibliography-entry': 'Bibliography entry is not cited',
    'possibly-unused-file': 'File is possibly unused',
    'circular-dependency': 'Circular file dependency',
  }
  return target ? `${titles[type] ?? type}: ${target}` : titles[type] ?? type
}

function analyzeProject(snapshot) {
  const entities = [
    ...snapshot.documents.map(item => ({ ...item, entityKind: 'document' })),
    ...snapshot.files.map(item => ({ ...item, entityKind: 'file' })),
  ]
  const entityByPath = new Map(entities.map(entity => [entity.path, entity]))
  const entityById = new Map(entities.map(entity => [entity.id, entity]))
  const availablePaths = new Set(entityByPath.keys())
  const graph = new _graph_builder_mjs__WEBPACK_IMPORTED_MODULE_5__.GraphBuilder()
  const issues = new _issue_collector_mjs__WEBPACK_IMPORTED_MODULE_6__.IssueCollector()
  const issueGroups = {
    missing: new Set(),
    unused: new Set(),
    duplicate: new Set(),
    citationMissing: new Set(),
    citationUnused: new Set(),
    citationDuplicate: new Set(),
    circular: new Set(),
  }
  const addIssue = (key, issue, entryPointId, groups = []) => {
    issues.add(
      key,
      { ...issue, title: issueTitle(issue.type, issue.target) },
      entryPointId
    )
    for (const group of groups) issueGroups[group].add(key)
  }

  for (const entity of entities) {
    graph.addNode({
      id: fileNodeId(entity.path),
      kind: entity.entityKind,
      label: node_path__WEBPACK_IMPORTED_MODULE_0__.posix.basename(entity.path),
      path: entity.path,
      entityId: entity.id,
    })
  }

  const latexByPath = new Map()
  for (const doc of snapshot.documents) {
    if (isTexLike(doc.path)) latexByPath.set(doc.path, (0,_latex_extractor_mjs__WEBPACK_IMPORTED_MODULE_1__.extractLatex)(doc))
  }

  const bibByPath = new Map()
  for (const doc of snapshot.documents) {
    if (isBibliography(doc.path)) bibByPath.set(doc.path, (0,_bibtex_extractor_mjs__WEBPACK_IMPORTED_MODULE_2__.extractBibtex)(doc))
  }
  for (const file of snapshot.binaryBibliographies) {
    bibByPath.set(file.path, (0,_bibtex_extractor_mjs__WEBPACK_IMPORTED_MODULE_2__.extractBibtex)(file))
  }

  const relationsByPath = new Map()
  const ambiguousReferences = []
  for (const [sourcePath, parsed] of latexByPath) {
    const relations = resolveRelations(parsed, availablePaths)
    relationsByPath.set(sourcePath, relations)
    for (const [kind, items] of [
      ['include', relations.includes],
      ['bibliography', relations.bibliographies],
    ]) {
      for (const item of items) {
        addResolvedRelationToGraph(graph, sourcePath, kind, item)
        if (item.resolution.status === 'ambiguous') {
          ambiguousReferences.push({
            kind,
            target: item.target,
            candidates: item.resolution.candidates,
            location: item.location,
          })
        }
      }
    }

    const environmentsByFrom = new Map(
      parsed.environments.map(environment => [environment.from, environment])
    )
    for (const environment of parsed.environments) {
      relationNode(
        graph,
        environment.kind,
        environment,
        sourcePath,
        environmentLabel(environment, sourcePath),
        environment.name
      )
    }
    for (const item of relations.figures) {
      addFigureResourceToGraph(
        graph,
        sourcePath,
        item,
        environmentsByFrom,
        entityByPath
      )
      if (item.resolution.status === 'ambiguous') {
        ambiguousReferences.push({
          kind: 'figure',
          target: item.target,
          candidates: item.resolution.candidates,
          location: item.location,
        })
      }
    }

    for (const label of parsed.labels) {
      relationNode(graph, 'label', label, sourcePath, label.key)
    }
    for (const reference of parsed.references) {
      graph.addNode({
        id: occurrenceNodeId(
          'reference',
          reference.location,
          reference.key
        ),
        kind: 'reference',
        label: reference.key,
        path: sourcePath,
        location: reference.location,
      })
    }
    for (const citation of parsed.citations) {
      if (citation.key === '*') continue
      const citationId = occurrenceNodeId(
        'citation-occurrence',
        citation.location,
        citation.key
      )
      const groupId = citationGroupNodeId(citation)
      graph.addNode({
        id: citationId,
        kind: 'citation-occurrence',
        label: sourcePath + ':' + citation.location.line,
        path: sourcePath,
        location: citation.location,
        parentId: groupId,
      })
      graph.addEdge({ kind: 'contains', from: groupId, to: citationId })
    }
  }

  for (const [bibPath, bibliography] of bibByPath) {
    for (const entry of bibliography.entries) {
      relationNode(graph, 'bibliography-entry', entry, bibPath, entry.key)
    }
  }

  const selectedRoots = snapshot.entryPointIds
    .map(id => entityById.get(id))
    .filter(Boolean)
  const scopes = selectedRoots.map(root => ({
    root,
    ...collectScope(root.path, latexByPath, relationsByPath, bibByPath),
  }))
  const reachableDocuments = new Set()
  const reachableImages = new Set()
  const reachableBibliographies = new Set()
  const usedLabels = new Set()
  const usedBibliographyEntries = new Set()
  const createdCitationGroups = new Set()
  const resolvedCitationGroups = new Set()
  const includeAdjacency = new Map()

  for (const scope of scopes) {
    for (const value of scope.documents) reachableDocuments.add(value)
    for (const value of scope.images) reachableImages.add(value)
    for (const value of scope.bibliographies) reachableBibliographies.add(value)

    const labelsByKey = new Map()
    const captionsByKind = {
      figure: new Map(),
      table: new Map(),
    }
    const entriesByKey = new Map()
    const entriesByTitle = new Map()
    const references = []
    const citations = []
    let labelsIncomplete = false

    for (const sourcePath of scope.documents) {
      const parsed = latexByPath.get(sourcePath)
      const relations = relationsByPath.get(sourcePath)
      if (!includeAdjacency.has(sourcePath)) includeAdjacency.set(sourcePath, [])

      for (const relation of relations.includes) {
        if (
          relation.resolution.status === 'resolved' &&
          scope.documents.has(relation.resolution.path)
        ) {
          includeAdjacency.get(sourcePath).push(relation.resolution.path)
        }
      }

      for (const [type, items] of [
        ['missing-file', relations.includes],
        ['missing-figure', relations.figures],
        ['missing-bibliography', relations.bibliographies],
      ]) {
        for (const item of items) {
          if (item.resolution.status !== 'missing') continue
          const kind = type.replace('missing-', '')
          const commandId =
            type === 'missing-figure'
              ? figureResourceNodeId(item)
              : occurrenceNodeId(
                  kind === 'file' ? 'include' : kind,
                  item.location,
                  item.target
                )
          const missingId = `missing:${kind === 'file' ? 'include' : kind}:${sourcePath}:${item.target}`
          const nodeIds =
            type === 'missing-figure'
              ? [commandId]
              : [commandId, missingId]
          addIssue(
            componentIssueKey(type, item.target),
            {
              type,
              status: 'missing',
              category: kind,
              target: item.target,
              locations: [item.location],
              nodeIds,
            },
            scope.root.id,
            ['missing']
          )
        }
      }

      for (const label of parsed.labels) {
        const values = labelsByKey.get(label.key) ?? []
        values.push(label)
        labelsByKey.set(label.key, values)
      }
      for (const environment of parsed.environments) {
        for (const caption of environment.captions) {
          const normalized = normalizedCaption(caption.value)
          if (!normalized) continue
          const captions = captionsByKind[environment.kind]
          const values = captions.get(normalized) ?? []
          values.push({
            caption,
            environment,
          })
          captions.set(normalized, values)
        }
      }
      references.push(...parsed.references)
      citations.push(...parsed.citations)
      labelsIncomplete ||= parsed.dynamicReferences.some(
        reference => reference.kind === 'label'
      )
      for (const entry of parsed.bibliographyEntries) {
        collectBibliographyEntry(entriesByKey, entriesByTitle, entry)
      }
    }

    for (const bibPath of scope.bibliographies) {
      const bibliography = bibByPath.get(bibPath)
      if (!bibliography || bibliography.unavailable) continue
      for (const entry of bibliography.entries) {
        collectBibliographyEntry(entriesByKey, entriesByTitle, entry)
      }
    }

    for (const [key, definitions] of labelsByKey) {
      if (definitions.length < 2) continue
      const nodeIds = definitions.map(definition =>
        occurrenceNodeId('label', definition.location, definition.key)
      )
      addIssue(
        componentIssueKey('duplicate-label', key),
        {
          type: 'duplicate-label',
          status: 'duplicate',
          category: 'label',
          target: key,
          locations: definitions.map(item => item.location),
          nodeIds,
        },
        scope.root.id,
        ['duplicate']
      )
    }

    for (const kind of ['figure', 'table']) {
      for (const [normalizedValue, definitions] of captionsByKind[kind]) {
        if (definitions.length < 2) continue
        const type = `duplicate-${kind}-caption`
        addIssue(
          componentIssueKey(type, normalizedValue),
          {
            type,
            status: 'duplicate',
            category: kind,
            target: definitions[0].caption.value,
            locations: definitions.map(item => item.caption.location),
            nodeIds: definitions.map(item =>
              environmentNodeId(item.environment)
            ),
          },
          scope.root.id,
          ['duplicate']
        )
      }
    }

    for (const reference of references) {
      const definitions = labelsByKey.get(reference.key) ?? []
      const referenceId = occurrenceNodeId(
        'reference',
        reference.location,
        reference.key
      )
      if (definitions.length === 0) {
        attachNodeToFile(graph, referenceId, reference.location.path)
        if (!labelsIncomplete) {
          addIssue(
            componentIssueKey('missing-reference', reference.key),
            {
              type: 'missing-reference',
              status: 'missing',
              category: 'reference',
              target: reference.key,
              locations: [reference.location],
              nodeIds: [referenceId],
            },
            scope.root.id,
            ['missing']
          )
        }
        continue
      }

      graph.addNode({
        id: referenceId,
        label: `${reference.location.path}:${reference.location.line}`,
      })
      for (const definition of definitions) {
        const labelId = occurrenceNodeId(
          'label',
          definition.location,
          definition.key
        )
        usedLabels.add(labelId)
        graph.addEdge({
          kind: 'references',
          from: labelId,
          to: referenceId,
        })
      }
    }

    const duplicateKeyEntrySignatures = new Set()
    for (const [key, entries] of entriesByKey) {
      if (entries.length < 2) continue
      const entrySignature = bibliographyEntrySignature(entries)
      duplicateKeyEntrySignatures.add(entrySignature)
      const nodeIds = entries.map(entry =>
        occurrenceNodeId('bibliography-entry', entry.location, entry.key)
      )
      addIssue(
        componentIssueKey('duplicate-bibliography-key', key),
        {
          type: 'duplicate-bibliography-key',
          status: 'duplicate',
          category: 'bibliography',
          target: key,
          locations: entries.map(item => item.location),
          nodeIds,
        },
        scope.root.id,
        ['duplicate', 'citationDuplicate']
      )
    }

    for (const [normalizedTitle, entries] of entriesByTitle) {
      if (entries.length < 2) continue
      const entrySignature = bibliographyEntrySignature(entries)
      if (duplicateKeyEntrySignatures.has(entrySignature)) continue
      const nodeIds = entries.map(entry =>
        occurrenceNodeId('bibliography-entry', entry.location, entry.key)
      )
      addIssue(
        componentIssueKey('duplicate-bibliography-title', normalizedTitle),
        {
          type: 'duplicate-bibliography-title',
          status: 'duplicate',
          category: 'bibliography',
          target: entries[0].displayTitle,
          locations: entries.map(item => item.titleLocation),
          nodeIds,
        },
        scope.root.id,
        ['duplicate', 'citationDuplicate']
      )
    }

    const citeAll = citations.some(item => item.nocite && item.key === '*')
    if (citeAll) {
      for (const entries of entriesByKey.values()) {
        for (const entry of entries) {
          usedBibliographyEntries.add(
            occurrenceNodeId('bibliography-entry', entry.location, entry.key)
          )
        }
      }
    }
    for (const citation of citations) {
      if (citation.key === '*') continue
      const entries = entriesByKey.get(citation.key) ?? []
      const groupId = citationGroupNodeId(citation)
      if (
        !createdCitationGroups.has(groupId) ||
        (!resolvedCitationGroups.has(groupId) && entries.length > 0)
      ) {
        graph.addNode({
          id: groupId,
          kind: 'citation',
          label: citation.key,
          path: citation.location.path,
          location: entries[0]?.location ?? citation.location,
          parentId: fileNodeId(citation.location.path),
        })
        createdCitationGroups.add(groupId)
        if (entries.length > 0) resolvedCitationGroups.add(groupId)
      }
      graph.addEdge({
        kind: 'contains',
        from: fileNodeId(citation.location.path),
        to: groupId,
      })
      if (entries.length === 0 && !scope.bibliographyIncomplete) {
        addIssue(
          componentIssueKey('missing-citation', citation.key),
          {
            type: 'missing-citation',
            status: 'missing',
            category: 'citation',
            target: citation.key,
            locations: [citation.location],
            nodeIds: [groupId],
          },
          scope.root.id,
          ['missing', 'citationMissing']
        )
      }
      for (const entry of entries) {
        const entryId = occurrenceNodeId(
          'bibliography-entry',
          entry.location,
          entry.key
        )
        usedBibliographyEntries.add(entryId)
      }
    }

    const scopeCycles = (0,_cycles_mjs__WEBPACK_IMPORTED_MODULE_4__.findCycles)([...scope.documents], includeAdjacency)
    for (const cycle of scopeCycles) {
      const nodeIds = cycle.files.map(fileNodeId)
      const cycleEdges = buildCycleEdges(cycle, relationsByPath)
      addIssue(
        componentIssueKey('circular-dependency', cycle.files.join('|')),
        {
          type: 'circular-dependency',
          status: 'circular',
          category: 'file',
          target: cycle.path.slice(0, -1).join(' ↔ '),
          locations: [],
          nodeIds,
          cycleEdges,
        },
        scope.root.id,
        ['circular']
      )
    }
  }

  const reachableDynamicKinds = new Set(
    [...reachableDocuments].flatMap(sourcePath =>
      latexByPath
        .get(sourcePath)
        .dynamicReferences.map(reference => reference.kind)
    )
  )

  for (const sourcePath of reachableDocuments) {
    const parsed = latexByPath.get(sourcePath)
    for (const label of parsed.labels) {
      const nodeId = occurrenceNodeId('label', label.location, label.key)
      if (
        usedLabels.has(nodeId) ||
        reachableDynamicKinds.has('reference')
      ) {
        continue
      }
      addIssue(
        componentIssueKey('unreferenced-label', label.key),
        {
          type: 'unreferenced-label',
          status: 'unreferenced',
          category: 'label',
          target: label.key,
          locations: [label.location],
          nodeIds: [nodeId],
        },
        null,
        ['unused']
      )
    }
    for (const environment of parsed.environments) {
      const nodeId = occurrenceNodeId(
        environment.kind,
        environment.location,
        environment.name
      )
      if (environment.labels.length === 0) {
        const type = `unlabeled-${environment.kind}`
        addIssue(
          `${type}:${environment.location.entityId}:${environment.location.from}`,
          {
            type,
            status: 'unreferenced',
            category: environment.kind,
            target: environment.name,
            locations: [environment.location],
            nodeIds: [nodeId],
          },
          null,
          ['unused']
        )
      } else if (
        !reachableDynamicKinds.has('reference') &&
        environment.labels.every(labelKey => {
          const label = parsed.labels.find(item => item.key === labelKey)
          return (
            !label ||
            !usedLabels.has(
              occurrenceNodeId('label', label.location, label.key)
            )
          )
        })
      ) {
        const type = `unreferenced-${environment.kind}`
        addIssue(
          `${type}:${environment.location.entityId}:${environment.location.from}`,
          {
            type,
            status: 'unreferenced',
            category: environment.kind,
            target: environment.labels.join(', '),
            locations: [environment.location],
            nodeIds: [nodeId],
          },
          null,
          ['unused']
        )
      }
    }
  }

  for (const bibPath of reachableBibliographies) {
    const bibliography = bibByPath.get(bibPath)
    if (!bibliography || bibliography.unavailable) continue
    for (const entry of bibliography.entries) {
      const nodeId = occurrenceNodeId(
        'bibliography-entry',
        entry.location,
        entry.key
      )
      if (
        usedBibliographyEntries.has(nodeId) ||
        reachableDynamicKinds.has('citation')
      ) {
        continue
      }
      addIssue(
        componentIssueKey('unused-bibliography-entry', entry.key),
        {
          type: 'unused-bibliography-entry',
          status: 'unused',
          category: 'bibliography',
          target: entry.key,
          locations: [entry.location],
          nodeIds: [nodeId],
        },
        null,
        ['unused', 'citationUnused']
      )
    }
  }

  for (const sourcePath of reachableDocuments) {
    const parsed = latexByPath.get(sourcePath)
    for (const entry of parsed.bibliographyEntries) {
      const nodeId = occurrenceNodeId(
        'bibliography-entry',
        entry.location,
        entry.key
      )
      if (
        usedBibliographyEntries.has(nodeId) ||
        reachableDynamicKinds.has('citation')
      ) {
        continue
      }
      addIssue(
        componentIssueKey('unused-bibliography-entry', entry.key),
        {
          type: 'unused-bibliography-entry',
          status: 'unused',
          category: 'bibliography',
          target: entry.key,
          locations: [entry.location],
          nodeIds: [nodeId],
        },
        null,
        ['unused', 'citationUnused']
      )
    }
  }

  for (const entity of entities) {
    if (
      IMAGE_EXTENSIONS.includes(extension(entity.path)) &&
      !reachableImages.has(entity.path) &&
      !reachableDynamicKinds.has('includegraphics') &&
      !reachableDynamicKinds.has('includesvg') &&
      !reachableDynamicKinds.has('graphicspath')
    ) {
      addIssue(
        componentIssueKey('unreferenced-figure', entity.path),
        {
          type: 'unreferenced-figure',
          status: 'unreferenced',
          category: 'figure',
          target: entity.path,
          locations: [],
          nodeIds: [fileNodeId(entity.path)],
        },
        null,
        ['unused']
      )
    }

    const eligible = isTexLike(entity.path) || isBibliography(entity.path)
    const reachable =
      reachableDocuments.has(entity.path) ||
      reachableBibliographies.has(entity.path)
    if (eligible && !reachable) {
      addIssue(
        componentIssueKey('possibly-unused-file', entity.path),
        {
          type: 'possibly-unused-file',
          status: 'unused',
          category: 'file',
          target: entity.path,
          locations: [],
          nodeIds: [fileNodeId(entity.path)],
        },
        null,
        ['unused']
      )
    }
  }

  const cycles = (0,_cycles_mjs__WEBPACK_IMPORTED_MODULE_4__.findCycles)([...reachableDocuments], includeAdjacency)
  const finalized = issues.finalize()
  for (const issue of Object.values(finalized.byId)) {
    graph.markNodes(issue.nodeIds, issue.status)
  }
  const idsFor = group =>
    [...issueGroups[group]]
      .map(key => finalized.keyToId.get(key))
      .filter(Boolean)

  const reachableEntityPaths = new Set([
    ...reachableDocuments,
    ...reachableImages,
    ...reachableBibliographies,
  ])
  const reachableLatex = [...reachableDocuments]
    .map(filePath => latexByPath.get(filePath))
    .filter(Boolean)
  const dynamicReferences = reachableLatex.flatMap(item =>
    item.dynamicReferences.map(reference => ({
      ...reference,
      sourcePath: item.path,
    }))
  )
  const parseErrors = [
    ...reachableLatex,
    ...[...reachableBibliographies]
      .map(filePath => bibByPath.get(filePath))
      .filter(Boolean),
  ]
    .filter(item => item.parseErrorCount > 0)
    .map(item => ({ path: item.path, count: item.parseErrorCount }))
  const skippedFiles = [...bibByPath.values()]
    .filter(
      item => item.unavailable && reachableBibliographies.has(item.path)
    )
    .map(item => ({ path: item.path, reason: item.skippedReason }))
  const reachableAmbiguousReferences = ambiguousReferences.filter(item =>
    reachableDocuments.has(item.location.path)
  )

  return {
    entryPoints: selectedRoots.map(root => ({ id: root.id, path: root.path })),
    overview: {
      fileCount: reachableEntityPaths.size,
      figureCount: reachableLatex.reduce(
        (count, item) =>
          count + item.environments.filter(env => env.kind === 'figure').length,
        0
      ),
      tableCount: reachableLatex.reduce(
        (count, item) =>
          count + item.environments.filter(env => env.kind === 'table').length,
        0
      ),
      citationCount: reachableLatex.reduce(
        (count, item) => count + item.citations.length,
        0
      ),
      missing: idsFor('missing').length,
      unusedUnreferenced: idsFor('unused').length,
      duplicate: idsFor('duplicate').length,
      circular: idsFor('circular').length,
    },
    graph: graph.serialize({
      roots: selectedRoots.map(root => fileNodeId(root.path)),
      cycles: cycles.map(cycle => cycle.path.map(fileNodeId)),
    }),
    issues: { byId: finalized.byId, truncated: finalized.truncated },
    views: {
      missing: idsFor('missing'),
      unused: idsFor('unused'),
      duplicate: idsFor('duplicate'),
      circular: idsFor('circular'),
      citation: {
        missing: idsFor('citationMissing'),
        unused: idsFor('citationUnused'),
        duplicate: idsFor('citationDuplicate'),
      },
    },
    coverage: {
      parsedFiles: reachableLatex.length + reachableBibliographies.size,
      parseErrors,
      dynamicReferences: dynamicReferences.slice(0, 500),
      ambiguousReferences: reachableAmbiguousReferences.slice(0, 500),
      skippedFiles,
      suppressedCitationChecks: scopes
        .filter(scope => scope.bibliographyIncomplete)
        .map(scope => scope.root.id),
      truncated:
        dynamicReferences.length > 500 ||
        reachableAmbiguousReferences.length > 500 ||
        finalized.truncated,
    },
  }
}

/* harmony default export */ const __WEBPACK_DEFAULT_EXPORT__ = (analyzeProject);


/***/ },

/***/ "./modules/project-inspection/app/src/analyzer/bibtex-extractor.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   extractBibtex: () => (/* binding */ extractBibtex)
/* harmony export */ });
/* harmony import */ var _frontend_js_features_source_editor_lezer_bibtex_bibtex_mjs__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("../../frontend/js/features/source-editor/lezer-bibtex/bibtex.mjs");
/* harmony import */ var _source_location_mjs__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/source-location.mjs");



function staticValue(source, valueNode) {
  const parts = []
  const cursor = valueNode.cursor()
  if (!cursor.firstChild()) return ''
  do {
    if (cursor.name === 'StringName') return undefined
    if (cursor.name === 'NumberLiteral') {
      parts.push(source.slice(cursor.from, cursor.to))
      continue
    }
    if (cursor.name !== 'StringLiteral') continue
    const literal = cursor.node.getChild('StringContents')
    parts.push(literal ? source.slice(literal.from, literal.to) : '')
  } while (cursor.nextSibling())
  return parts.join('')
}

function extractEntry(file, lineStarts, entryNode) {
  const body = entryNode.getChild('EntryBody')
  const citationKey = body?.getChild('CitationKey')
  if (!body || !citationKey) return undefined
  const key = file.content.slice(citationKey.from, citationKey.to).trim()
  if (!key) return undefined

  const entry = {
    key,
    location: (0,_source_location_mjs__WEBPACK_IMPORTED_MODULE_1__.createLocation)(file.content, lineStarts, {
      entityId: file.id,
      path: file.path,
      from: citationKey.from,
      to: citationKey.to,
    }),
  }
  for (const field of body.getChildren('Field')) {
    const fieldName = field.getChild('FieldName')
    const value = field.getChild('Value')
    if (
      !fieldName ||
      !value ||
      file.content.slice(fieldName.from, fieldName.to).toLowerCase() !==
        'title'
    ) {
      continue
    }
    const title = staticValue(file.content, value)
    if (title == null) return entry
    entry.title = title
    entry.titleLocation = (0,_source_location_mjs__WEBPACK_IMPORTED_MODULE_1__.createLocation)(file.content, lineStarts, {
      entityId: file.id,
      path: file.path,
      from: field.from,
      to: field.to,
    })
    return entry
  }
  return entry
}

function extractBibtex(file) {
  if (file.content == null) {
    return {
      path: file.path,
      entityId: file.id,
      entries: [],
      parseErrorCount: 0,
      unavailable: true,
      skippedReason: file.skippedReason ?? 'content-unavailable',
    }
  }

  const lineStarts = (0,_source_location_mjs__WEBPACK_IMPORTED_MODULE_1__.createLineIndex)(file.content)
  const tree = _frontend_js_features_source_editor_lezer_bibtex_bibtex_mjs__WEBPACK_IMPORTED_MODULE_0__.parser.parse(file.content)
  const entries = []
  let parseErrorCount = 0

  tree.iterate({
    enter(node) {
      if (node.type.isError) {
        parseErrorCount++
      } else if (node.type.name === 'Entry') {
        const entry = extractEntry(file, lineStarts, node.node)
        if (entry) entries.push(entry)
      }
    },
  })

  return {
    path: file.path,
    entityId: file.id,
    entries,
    parseErrorCount,
    unavailable: false,
  }
}


/***/ },

/***/ "./modules/project-inspection/app/src/analyzer/cycles.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   findCycles: () => (/* binding */ findCycles),
/* harmony export */   findStronglyConnectedComponents: () => (/* binding */ findStronglyConnectedComponents)
/* harmony export */ });
function findStronglyConnectedComponents(nodes, adjacency) {
  let index = 0
  const stack = []
  const onStack = new Set()
  const indices = new Map()
  const lowLinks = new Map()
  const components = []

  function visit(node) {
    indices.set(node, index)
    lowLinks.set(node, index)
    index++
    stack.push(node)
    onStack.add(node)

    for (const neighbor of adjacency.get(node) ?? []) {
      if (!indices.has(neighbor)) {
        visit(neighbor)
        lowLinks.set(
          node,
          Math.min(lowLinks.get(node), lowLinks.get(neighbor))
        )
      } else if (onStack.has(neighbor)) {
        lowLinks.set(
          node,
          Math.min(lowLinks.get(node), indices.get(neighbor))
        )
      }
    }

    if (lowLinks.get(node) === indices.get(node)) {
      const component = []
      let current
      do {
        current = stack.pop()
        onStack.delete(current)
        component.push(current)
      } while (current !== node)
      components.push(component)
    }
  }

  for (const node of nodes) {
    if (!indices.has(node)) visit(node)
  }
  return components
}

function representativeCycle(component, adjacency) {
  const allowed = new Set(component)
  const start = component[0]
  const path = []
  const inPath = new Set()

  function search(node) {
    path.push(node)
    inPath.add(node)
    for (const neighbor of adjacency.get(node) ?? []) {
      if (!allowed.has(neighbor)) continue
      if (neighbor === start) return [...path, start]
      if (!inPath.has(neighbor)) {
        const result = search(neighbor)
        if (result) return result
      }
    }
    path.pop()
    inPath.delete(node)
    return null
  }

  return search(start) ?? [...component, component[0]]
}

function findCycles(nodes, adjacency) {
  return findStronglyConnectedComponents(nodes, adjacency)
    .filter(
      component =>
        component.length > 1 ||
        (adjacency.get(component[0]) ?? []).includes(component[0])
    )
    .map((component, index) => ({
      id: `cycle-${index + 1}`,
      files: [...component].sort(),
      path: representativeCycle(component, adjacency),
    }))
}


/***/ },

/***/ "./modules/project-inspection/app/src/analyzer/graph-builder.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   GraphBuilder: () => (/* binding */ GraphBuilder)
/* harmony export */ });
const STATUS_PRIORITY = {
  normal: 0,
  unused: 1,
  unreferenced: 1,
  duplicate: 2,
  missing: 3,
  circular: 4,
}

class GraphBuilder {
  constructor() {
    this.nodes = new Map()
    this.edges = new Map()
  }

  addNode(node) {
    const existing = this.nodes.get(node.id)
    if (!existing) {
      this.nodes.set(node.id, { status: 'normal', ...node })
      return node.id
    }
    this.nodes.set(node.id, { ...existing, ...node, status: existing.status })
    return node.id
  }

  addEdge(edge) {
    const id = edge.id || `${edge.kind}:${edge.from}:${edge.to}`
    if (!this.edges.has(id)) {
      this.edges.set(id, { ...edge, id })
    }
    return id
  }

  markNodes(nodeIds, status) {
    for (const nodeId of nodeIds || []) {
      const node = this.nodes.get(nodeId)
      if (
        node &&
        (STATUS_PRIORITY[status] || 0) >
          (STATUS_PRIORITY[node.status] || 0)
      ) {
        node.status = status
      }
    }
  }

  serialize({ roots, cycles, maxNodes = 5000, maxEdges = 10000 }) {
    const sortedNodes = [...this.nodes.values()].sort((a, b) =>
      a.id.localeCompare(b.id)
    )
    const sortedEdges = [...this.edges.values()].sort((a, b) =>
      a.id.localeCompare(b.id)
    )
    const outgoing = new Map()
    for (const edge of sortedEdges) {
      const edges = outgoing.get(edge.from) ?? []
      edges.push(edge)
      outgoing.set(edge.from, edges)
    }

    const orderedIds = []
    const visited = new Set()
    const queue = [...roots]
    while (queue.length > 0) {
      const nodeId = queue.shift()
      if (visited.has(nodeId) || !this.nodes.has(nodeId)) continue
      visited.add(nodeId)
      orderedIds.push(nodeId)
      for (const edge of outgoing.get(nodeId) ?? []) queue.push(edge.to)
    }
    for (const node of sortedNodes) {
      if (!visited.has(node.id)) orderedIds.push(node.id)
    }

    const allNodes = orderedIds.map(nodeId => this.nodes.get(nodeId))
    const includedNodes = allNodes.slice(0, maxNodes)
    const includedIds = new Set(includedNodes.map(node => node.id))
    const eligibleEdges = sortedEdges.filter(
      edge => includedIds.has(edge.from) && includedIds.has(edge.to)
    )
    const includedEdges = eligibleEdges.slice(0, maxEdges)

    return {
      roots: roots.filter(root => includedIds.has(root)),
      nodes: includedNodes,
      edges: includedEdges,
      cycles: cycles.filter(cycle =>
        cycle.every(nodeId => includedIds.has(nodeId))
      ),
      truncated:
        includedNodes.length < allNodes.length ||
        includedEdges.length < eligibleEdges.length,
    }
  }
}


/***/ },

/***/ "./modules/project-inspection/app/src/analyzer/issue-collector.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   IssueCollector: () => (/* binding */ IssueCollector)
/* harmony export */ });
const STATUS_PRIORITY = {
  normal: 0,
  unused: 1,
  unreferenced: 1,
  duplicate: 2,
  missing: 3,
  circular: 4,
}

function unique(values = []) {
  return [...new Set(values.filter(Boolean))]
}

class IssueCollector {
  constructor() {
    this.issues = new Map()
  }

  add(key, issue, entryPointId) {
    const existing = this.issues.get(key)
    if (!existing) {
      this.issues.set(key, {
        ...issue,
        locations: [...(issue.locations || [])],
        nodeIds: unique(issue.nodeIds),
        entryPoints: entryPointId ? [entryPointId] : [],
        cycleEdges: issue.cycleEdges ? [...issue.cycleEdges] : undefined,
      })
      return
    }

    existing.locations.push(...(issue.locations || []))
    existing.nodeIds = unique([...existing.nodeIds, ...(issue.nodeIds || [])])
    existing.entryPoints = unique([
      ...existing.entryPoints,
      ...(entryPointId ? [entryPointId] : []),
    ])
    if (issue.cycleEdges?.length) {
      existing.cycleEdges = [
        ...(existing.cycleEdges || []),
        ...issue.cycleEdges,
      ]
    }
    if (
      (STATUS_PRIORITY[issue.status] || 0) >
      (STATUS_PRIORITY[existing.status] || 0)
    ) {
      existing.status = issue.status
    }
  }

  finalize(maxIssues = 5000) {
    const sorted = [...this.issues.entries()].sort(([keyA], [keyB]) =>
      keyA.localeCompare(keyB)
    )
    const byId = {}
    const keyToId = new Map()
    let truncated = sorted.length > maxIssues

    sorted.slice(0, maxIssues).forEach(([key, issue], index) => {
      const id = `issue-${index + 1}`
      const locations = deduplicateLocations(issue.locations)
      if (locations.length > 200) truncated = true
      keyToId.set(key, id)
      byId[id] = {
        id,
        ...issue,
        locations: locations.slice(0, 200),
        nodeIds: unique(issue.nodeIds),
        entryPoints: unique(issue.entryPoints),
        cycleEdges: issue.cycleEdges
          ? deduplicateCycleEdges(issue.cycleEdges)
          : undefined,
      }
    })

    return { byId, keyToId, truncated }
  }
}

function deduplicateLocations(locations) {
  const seen = new Set()
  return locations
    .filter(location => {
      const key = `${location.entityId}:${location.from}:${location.to}`
      if (seen.has(key)) {
        return false
      }
      seen.add(key)
      return true
    })
    .sort(compareLocations)
}

function compareLocations(left, right) {
  return (
    left.path.localeCompare(right.path) ||
    left.line - right.line ||
    left.column - right.column ||
    left.from - right.from
  )
}

function deduplicateCycleEdges(cycleEdges) {
  const seen = new Set()
  return cycleEdges
    .filter(edge => {
      const { location } = edge
      const key = JSON.stringify([
        edge.from,
        edge.to,
        location.entityId,
        location.from,
        location.to,
      ])
      if (seen.has(key)) return false
      seen.add(key)
      return true
    })
    .sort((left, right) => compareLocations(left.location, right.location))
}


/***/ },

/***/ "./modules/project-inspection/app/src/analyzer/latex-extractor.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   extractLatex: () => (/* binding */ extractLatex)
/* harmony export */ });
/* harmony import */ var _frontend_js_features_source_editor_lezer_latex_latex_mjs__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("../../frontend/js/features/source-editor/lezer-latex/latex.mjs");
/* harmony import */ var _source_location_mjs__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/source-location.mjs");



const FILE_COMMANDS = new Map([
  ['Input', 'input'],
  ['Include', 'include'],
  ['Subfile', 'subfile'],
])
const PROJECT_CLASS_COMMANDS = new Set([
  'loadclass',
  'loadclasswithoptions',
])
const FIGURE_COMMANDS = new Map([
  ['IncludeGraphics', 'includegraphics'],
  ['IncludeSvg', 'includesvg'],
])
const SUPPORTED_ENVIRONMENTS = new Map([
  ['figure', 'figure'],
  ['figure*', 'figure'],
  ['table', 'table'],
  ['table*', 'table'],
  ['longtable', 'table'],
])
const ENVIRONMENT_NODE_TYPES = new Set([
  'FigureEnvironment',
  'TableEnvironment',
  'TabularEnvironment',
  'Environment',
])

function commandName(raw) {
  return raw.match(/^\\([A-Za-z@]+)/)?.[1]?.toLowerCase() ?? ''
}

function topLevelBraceArguments(raw) {
  const results = []
  let bracketDepth = 0
  let braceDepth = 0
  let start = -1
  let escaped = false

  for (let index = 0; index < raw.length; index++) {
    const char = raw[index]
    if (escaped) {
      escaped = false
      continue
    }
    if (char === '\\') {
      escaped = true
      continue
    }
    if (char === '[' && braceDepth === 0) {
      bracketDepth++
      continue
    }
    if (char === ']' && braceDepth === 0 && bracketDepth > 0) {
      bracketDepth--
      continue
    }
    if (bracketDepth > 0) continue
    if (char === '{') {
      if (braceDepth === 0) start = index + 1
      braceDepth++
    } else if (char === '}' && braceDepth > 0) {
      braceDepth--
      if (braceDepth === 0 && start >= 0) {
        results.push({
          value: raw.slice(start, index),
          from: start,
          to: index,
        })
        start = -1
      }
    }
  }
  return results
}

function staticValue(value) {
  const trimmed = value.trim()
  if (!trimmed || /[\\#]/.test(trimmed)) return null
  return trimmed
}

function splitStaticList(value) {
  return value
    .split(',')
    .map(item => staticValue(item))
    .filter(Boolean)
}

function splitStaticListWithOffsets(value) {
  const results = []
  let offset = 0
  for (const item of value.split(',')) {
    const key = staticValue(item)
    if (key) {
      const from = offset + item.indexOf(key)
      results.push({ key, from, to: from + key.length })
    }
    offset += item.length + 1
  }
  return results
}

function commandTarget(raw) {
  const args = topLevelBraceArguments(raw)
  if (args.length > 0) return staticValue(args.at(-1).value)
  const bare = raw.replace(/^\\[A-Za-z@]+\s*/, '').trim()
  return staticValue(bare)
}

function commandLocation(source, lineStarts, doc, node, raw, value) {
  const relativeOffset = value ? raw.indexOf(value) : 0
  const from = node.from + Math.max(0, relativeOffset)
  return (0,_source_location_mjs__WEBPACK_IMPORTED_MODULE_1__.createLocation)(source, lineStarts, {
    entityId: doc.id,
    path: doc.path,
    from,
    to: value ? from + value.length : node.to,
  })
}

function fullCommandLocation(source, lineStarts, doc, node) {
  return (0,_source_location_mjs__WEBPACK_IMPORTED_MODULE_1__.createLocation)(source, lineStarts, {
    entityId: doc.id,
    path: doc.path,
    from: node.from,
    to: node.to,
  })
}

function parseGraphicPaths(value) {
  const paths = []
  for (const argument of topLevelBraceArguments(value)) {
    const path = staticValue(argument.value)
    if (path) paths.push(path)
  }
  return paths
}

function extractLatex(doc) {
  const source = doc.content
  const lineStarts = (0,_source_location_mjs__WEBPACK_IMPORTED_MODULE_1__.createLineIndex)(source)
  const tree = _frontend_js_features_source_editor_lezer_latex_latex_mjs__WEBPACK_IMPORTED_MODULE_0__.parser.parse(source)
  const result = {
    path: doc.path,
    entityId: doc.id,
    includes: [],
    figures: [],
    bibliographyFiles: [],
    captions: [],
    labels: [],
    references: [],
    citations: [],
    bibliographyEntries: [],
    environments: [],
    graphicPaths: [],
    dynamicReferences: [],
    parseErrorCount: 0,
  }

  tree.iterate({
    enter(node) {
      const type = node.type.name
      if (node.type.isError) {
        result.parseErrorCount++
        return
      }

      const raw = source.slice(node.from, node.to)
      if (type === 'DocumentClass') {
        const target = commandTarget(raw)
        const location = commandLocation(
          source,
          lineStarts,
          doc,
          node,
          raw,
          target
        )
        if (target) {
          result.includes.push({
            relation: 'documentclass',
            target,
            location,
          })
        }
        return
      }

      const fileRelation = FILE_COMMANDS.get(type)
      if (fileRelation) {
        const target = commandTarget(raw)
        const location = commandLocation(
          source,
          lineStarts,
          doc,
          node,
          raw,
          target
        )
        if (target) {
          result.includes.push({ relation: fileRelation, target, location })
        } else {
          result.dynamicReferences.push({ kind: fileRelation, location })
        }
        return
      }

      const figureRelation = FIGURE_COMMANDS.get(type)
      if (figureRelation) {
        const target = commandTarget(raw)
        const location = commandLocation(
          source,
          lineStarts,
          doc,
          node,
          raw,
          target
        )
        if (target) {
          result.figures.push({ relation: figureRelation, target, location })
        } else {
          result.dynamicReferences.push({ kind: figureRelation, location })
        }
        return
      }

      if (type === 'BibliographyCommand') {
        const value = topLevelBraceArguments(raw).at(-1)?.value ?? ''
        const targets = splitStaticList(value)
        const location = commandLocation(
          source,
          lineStarts,
          doc,
          node,
          raw,
          value
        )
        if (targets.length > 0) {
          for (const target of targets) {
            result.bibliographyFiles.push({
              relation: 'bibliography',
              target,
              location,
            })
          }
        } else {
          result.dynamicReferences.push({ kind: 'bibliography', location })
        }
        return
      }

      if (type === 'Caption') {
        const value = topLevelBraceArguments(raw).at(-1)?.value ?? ''
        if (value.trim()) {
          result.captions.push({
            value: value.trim(),
            location: fullCommandLocation(source, lineStarts, doc, node),
          })
        }
        return
      }

      if (type === 'Label') {
        const value = topLevelBraceArguments(raw).at(-1)?.value ?? ''
        const key = staticValue(value)
        const location = fullCommandLocation(source, lineStarts, doc, node)
        if (key) {
          result.labels.push({
            key,
            location,
          })
        } else {
          result.dynamicReferences.push({ kind: 'label', location })
        }
        return
      }

      if (type === 'Ref') {
        const value = topLevelBraceArguments(raw).at(-1)?.value ?? ''
        const keys = splitStaticList(value)
        const location = fullCommandLocation(source, lineStarts, doc, node)
        if (keys.length > 0) {
          for (const key of keys) result.references.push({ key, location })
        } else {
          result.dynamicReferences.push({ kind: 'reference', location })
        }
        return
      }

      if (type === 'Cite') {
        const value = topLevelBraceArguments(raw).at(-1)?.value ?? ''
        const keys = splitStaticListWithOffsets(value)
        const valueOffset = Math.max(0, raw.indexOf(value))
        const name = commandName(raw)
        if (keys.length > 0) {
          for (const key of keys) {
            const from = node.from + valueOffset + key.from
            result.citations.push({
              key: key.key,
              nocite: name === 'nocite',
              location: (0,_source_location_mjs__WEBPACK_IMPORTED_MODULE_1__.createLocation)(source, lineStarts, {
                entityId: doc.id,
                path: doc.path,
                from,
                to: node.from + valueOffset + key.to,
              }),
            })
          }
        } else {
          const location = commandLocation(
            source,
            lineStarts,
            doc,
            node,
            raw,
            value
          )
          result.dynamicReferences.push({ kind: 'citation', location })
        }
        return
      }

      if (type === 'UnknownCommand') {
        const name = commandName(raw)
        const value = topLevelBraceArguments(raw).at(-1)?.value ?? ''
        const location = commandLocation(
          source,
          lineStarts,
          doc,
          node,
          raw,
          value
        )
        if (PROJECT_CLASS_COMMANDS.has(name)) {
          const target = staticValue(value)
          if (target) {
            result.includes.push({ relation: name, target, location })
          }
        } else if (name === 'addbibresource') {
          const target = staticValue(value)
          if (target) {
            result.bibliographyFiles.push({
              relation: 'addbibresource',
              target,
              location,
            })
          } else {
            result.dynamicReferences.push({
              kind: 'addbibresource',
              location,
            })
          }
        } else if (name === 'graphicspath') {
          const paths = parseGraphicPaths(value)
          result.graphicPaths.push(...paths)
          if (paths.length === 0) {
            result.dynamicReferences.push({ kind: 'graphicspath', location })
          }
        } else if (name === 'bibitem') {
          const key = staticValue(value)
          if (key) {
            result.bibliographyEntries.push({ key, location })
          } else {
            result.dynamicReferences.push({
              kind: 'bibliography-entry',
              location,
            })
          }
        }
        return
      }

      if (!ENVIRONMENT_NODE_TYPES.has(type)) return
      const name = raw.match(/^\\begin\s*\{([^}]+)\}/)?.[1]?.trim()
      const kind = SUPPORTED_ENVIRONMENTS.get(name)
      if (!kind) return
      result.environments.push({
        kind,
        name,
        from: node.from,
        to: node.to,
        location: (0,_source_location_mjs__WEBPACK_IMPORTED_MODULE_1__.createLocation)(source, lineStarts, {
          entityId: doc.id,
          path: doc.path,
          from: node.from,
          to: Math.min(node.to, node.from + raw.indexOf('}') + 1),
        }),
        captions: [],
        labels: [],
        figures: [],
      })
    },
  })

  for (const label of result.labels) {
    const containing = result.environments
      .filter(
        environment =>
          label.location.from >= environment.from &&
          label.location.to <= environment.to
      )
      .sort(
        (left, right) =>
          left.to - left.from - (right.to - right.from)
      )[0]
    if (containing) {
      containing.labels.push(label.key)
      label.environmentKind = containing.kind
    }
  }

  for (const caption of result.captions) {
    const containing = result.environments
      .filter(
        environment =>
          caption.location.from >= environment.from &&
          caption.location.to <= environment.to
      )
      .sort(
        (left, right) =>
          left.to - left.from - (right.to - right.from)
      )[0]
    if (containing) {
      containing.captions.push(caption)
      caption.environmentKind = containing.kind
    }
  }

  for (const figure of result.figures) {
    const containing = result.environments
      .filter(
        environment =>
          environment.kind === 'figure' &&
          figure.location.from >= environment.from &&
          figure.location.to <= environment.to
      )
      .sort(
        (left, right) =>
          left.to - left.from - (right.to - right.from)
      )[0]
    if (containing) {
      containing.figures.push(figure)
      figure.environmentFrom = containing.from
    }
  }

  return result
}


/***/ },

/***/ "./modules/project-inspection/app/src/analyzer/resource-resolver.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   isDynamicTarget: () => (/* binding */ isDynamicTarget),
/* harmony export */   resolveProjectPath: () => (/* binding */ resolveProjectPath)
/* harmony export */ });
/* harmony import */ var node_path__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("node:path");


const URL_OR_ABSOLUTE_PATH = /^(?:[a-z][a-z0-9+.-]*:|\/)/i

function normalizeCandidate(value) {
  const normalized = node_path__WEBPACK_IMPORTED_MODULE_0__.posix.normalize(value.replace(/^\.\//, ''))
  if (
    normalized === '..' ||
    normalized.startsWith('../') ||
    normalized.includes('/../')
  ) {
    return null
  }
  return normalized.replace(/^\/+/, '')
}

function isDynamicTarget(target) {
  return (
    !target ||
    URL_OR_ABSOLUTE_PATH.test(target) ||
    /[\\#{}]/.test(target)
  )
}

function resolveProjectPath({
  target,
  sourcePath,
  availablePaths,
  extensions = [],
  additionalRoots = [],
}) {
  if (isDynamicTarget(target)) {
    return { status: 'dynamic', candidates: [] }
  }

  const sourceDirectory = node_path__WEBPACK_IMPORTED_MODULE_0__.posix.dirname(sourcePath)
  const roots = [
    '',
    ...(sourceDirectory === '.' ? [] : [sourceDirectory]),
    ...additionalRoots.flatMap(root => {
      const fromRoot = normalizeCandidate(root)
      const fromSource = normalizeCandidate(node_path__WEBPACK_IMPORTED_MODULE_0__.posix.join(sourceDirectory, root))
      return [fromRoot, fromSource].filter(Boolean)
    }),
  ]
  const suffixes = node_path__WEBPACK_IMPORTED_MODULE_0__.posix.extname(target) ? [''] : ['', ...extensions]
  const candidates = []

  for (const root of roots) {
    for (const suffix of suffixes) {
      const candidate = normalizeCandidate(
        node_path__WEBPACK_IMPORTED_MODULE_0__.posix.join(root, `${target}${suffix}`)
      )
      if (
        candidate &&
        availablePaths.has(candidate) &&
        !candidates.includes(candidate)
      ) {
        candidates.push(candidate)
      }
    }
  }

  if (candidates.length === 1) {
    return { status: 'resolved', path: candidates[0], candidates }
  }
  if (candidates.length > 1) {
    return { status: 'ambiguous', candidates }
  }
  return { status: 'missing', candidates: [] }
}


/***/ },

/***/ "./modules/project-inspection/app/src/analyzer/source-location.mjs"
(__unused_webpack___webpack_module__, __webpack_exports__, __webpack_require__) {

__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   createLineIndex: () => (/* binding */ createLineIndex),
/* harmony export */   createLocation: () => (/* binding */ createLocation)
/* harmony export */ });
function createLineIndex(source) {
  const lineStarts = [0]
  for (let index = 0; index < source.length; index++) {
    if (source.charCodeAt(index) === 10) lineStarts.push(index + 1)
  }
  return lineStarts
}

function findLineIndex(lineStarts, offset) {
  let low = 0
  let high = lineStarts.length - 1
  while (low <= high) {
    const middle = Math.floor((low + high) / 2)
    if (lineStarts[middle] <= offset) {
      low = middle + 1
    } else {
      high = middle - 1
    }
  }
  return Math.max(0, high)
}

function createLocation(
  source,
  lineStarts,
  { entityId, path, from, to }
) {
  const lineIndex = findLineIndex(lineStarts, from)
  const safeTo = Math.max(from, Math.min(to, source.length))
  return {
    entityId,
    path,
    line: lineIndex + 1,
    column: from - lineStarts[lineIndex],
    from,
    to: safeTo,
    sourceText: source.slice(from, safeTo).replace(/\s+/g, ' ').slice(0, 240),
  }
}


/***/ }

/******/ 	});
/************************************************************************/
/******/ 	// The module cache
/******/ 	var __webpack_module_cache__ = {};
/******/ 	
/******/ 	// The require function
/******/ 	function __webpack_require__(moduleId) {
/******/ 		// Check if module is in cache
/******/ 		var cachedModule = __webpack_module_cache__[moduleId];
/******/ 		if (cachedModule !== undefined) {
/******/ 			return cachedModule.exports;
/******/ 		}
/******/ 		// Create a new module (and put it into the cache)
/******/ 		var module = __webpack_module_cache__[moduleId] = {
/******/ 			// no module.id needed
/******/ 			// no module.loaded needed
/******/ 			exports: {}
/******/ 		};
/******/ 	
/******/ 		// Execute the module function
/******/ 		__webpack_modules__[moduleId](module, module.exports, __webpack_require__);
/******/ 	
/******/ 		// Return the exports of the module
/******/ 		return module.exports;
/******/ 	}
/******/ 	
/************************************************************************/
/******/ 	/* webpack/runtime/define property getters */
/******/ 	(() => {
/******/ 		// define getter functions for harmony exports
/******/ 		__webpack_require__.d = (exports, definition) => {
/******/ 			for(var key in definition) {
/******/ 				if(__webpack_require__.o(definition, key) && !__webpack_require__.o(exports, key)) {
/******/ 					Object.defineProperty(exports, key, { enumerable: true, get: definition[key] });
/******/ 				}
/******/ 			}
/******/ 		};
/******/ 	})();
/******/ 	
/******/ 	/* webpack/runtime/hasOwnProperty shorthand */
/******/ 	(() => {
/******/ 		__webpack_require__.o = (obj, prop) => (Object.prototype.hasOwnProperty.call(obj, prop))
/******/ 	})();
/******/ 	
/******/ 	/* webpack/runtime/make namespace object */
/******/ 	(() => {
/******/ 		// define __esModule on exports
/******/ 		__webpack_require__.r = (exports) => {
/******/ 			if(typeof Symbol !== 'undefined' && Symbol.toStringTag) {
/******/ 				Object.defineProperty(exports, Symbol.toStringTag, { value: 'Module' });
/******/ 			}
/******/ 			Object.defineProperty(exports, '__esModule', { value: true });
/******/ 		};
/******/ 	})();
/******/ 	
/************************************************************************/
var __webpack_exports__ = {};
// This entry needs to be wrapped in an IIFE because it needs to be isolated against other modules in the chunk.
(() => {
__webpack_require__.r(__webpack_exports__);
/* harmony export */ __webpack_require__.d(__webpack_exports__, {
/* harmony export */   analyzeProject: () => (/* reexport safe */ _app_src_analyzer_analyze_project_mjs__WEBPACK_IMPORTED_MODULE_1__.analyzeProject),
/* harmony export */   resolveProjectPath: () => (/* reexport safe */ _app_src_analyzer_resource_resolver_mjs__WEBPACK_IMPORTED_MODULE_2__.resolveProjectPath)
/* harmony export */ });
/* harmony import */ var node_worker_threads__WEBPACK_IMPORTED_MODULE_0__ = __webpack_require__("node:worker_threads");
/* harmony import */ var _app_src_analyzer_analyze_project_mjs__WEBPACK_IMPORTED_MODULE_1__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/analyze-project.mjs");
/* harmony import */ var _app_src_analyzer_resource_resolver_mjs__WEBPACK_IMPORTED_MODULE_2__ = __webpack_require__("./modules/project-inspection/app/src/analyzer/resource-resolver.mjs");
// Worker entry for the project-inspection analysis engine.
//
// Dual mode (so the same file is the worker AND the unit-test target):
//   - imported as a module: exports the pure engine functions (no side
//     effects — parentPort is null on the main thread, so nothing runs);
//   - run inside node:worker_threads: answers { snapshot } messages with
//     { ok, result } / { ok:false, error:{ code, message } } (the
//     reference analysis-worker.mjs contract);
//   - run as a CLI process (`node dist/analyze-worker.mjs`, stdin snapshot
//     JSON, stdout { ok, ... } JSON): used by the Go web
//     (go/services/web/features/projectinspection) which spawns one
//     short-lived process per request — the OlliTeX equivalent of the
//     reference's per-request worker_threads (the Go runtime has no
//     in-process workers).
//
// Provenance: engine = services/web/modules/project-inspection/app/src/
// analyzer (upstream project-inspection module, AGPL-3.0 — see
// AGPLv3-LICENSE.txt in the module root). This file is OlliTeX glue.







function toErrorField (error) {
  return {
    code: error?.code ?? 'PROJECT_INSPECTION_ERROR',
    message: error?.message ?? 'Unknown analysis error'
  }
}

function run (snapshot) {
  return { ok: true, result: (0,_app_src_analyzer_analyze_project_mjs__WEBPACK_IMPORTED_MODULE_1__.analyzeProject)(snapshot) }
}

if (node_worker_threads__WEBPACK_IMPORTED_MODULE_0__.parentPort != null) {
  node_worker_threads__WEBPACK_IMPORTED_MODULE_0__.parentPort.on('message', (message) => {
    try {
      node_worker_threads__WEBPACK_IMPORTED_MODULE_0__.parentPort.postMessage(run(message.snapshot))
    } catch (error) {
      node_worker_threads__WEBPACK_IMPORTED_MODULE_0__.parentPort.postMessage({ ok: false, error: toErrorField(error) })
    }
  })
} else if (typeof process !== 'undefined' && process.argv[1]
  && process.argv[1].includes('analyze-worker')) {
  // CLI mode.
  let input = ''
  process.stdin.on('data', (chunk) => { input += chunk })
  process.stdin.on('end', () => {
    let output
    let exitCode = 0
    try {
      output = JSON.stringify(run(JSON.parse(input)))
    } catch (error) {
      output = JSON.stringify({ ok: false, error: toErrorField(error) })
      exitCode = 1
    }
    process.stdout.write(output)
    process.exitCode = exitCode
  })
}

})();

module.exports = __webpack_exports__;
/******/ })()
;