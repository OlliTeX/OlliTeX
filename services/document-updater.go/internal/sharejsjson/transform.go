// Transform machinery: ports json.transformComponent (oracle L289-580),
// the bootstrap transform/transformX (helpers.js L28-111) and the si/sd
// bridge into the vendored text type (via the sharejstext port).
//
// Vendored signature: transformComponent(dest, c, otherC, type). dest is a
// shared slice receiving the component(s) (receives nothing for a no-op).

package sharejsjson

import (
	"errors"

	sjtext "document-updater/internal/sharejstext"
)

var ErrBadSide = errors.New("type must be 'left' or 'right'")

var ErrMustBeString = errors.New("must be a string?")

func segIntSafe(p []any, i int) (int, bool) {
	if i < 0 || i >= len(p) {
		return 0, false
	}
	n, ok := p[i].(int)
	if !ok {
		return 0, false
	}
	return n, true
}

func segSet(p []any, i, n int) {
	if i >= 0 && i < len(p) {
		p[i] = n
	}
}

// textBridge converts both json components to vendored text components,
// runs the text transformComponent and converts the results back, applying
// the vendored `p = c.p.slice(0, common) + [tc.p]` shape.
func textBridge(dest *[]Component, c, otherC Component, common int, side string) error {
	convert := func(cc Component) sjtext.Component {
		off := 0
		if n, ok := segIntSafe(cc.P, len(cc.P)-1); ok {
			off = n
		}
		tc := sjtext.Component{P: off}
		if cc.SI != nil && *cc.SI != "" {
			tc.I = cc.SI
		} else {
			tc.D = cc.SD
		}
		return tc
	}
	tc1 := convert(c)
	tc2 := convert(otherC)
	var res []sjtext.Component
	if err := sjtext.TransformComponent(&res, tc1, tc2, side); err != nil {
		return err
	}
	for i := range res {
		tc := res[i]
		n := common
		if n > len(c.P) {
			n = len(c.P)
		}
		jc := Component{P: make([]any, 0, n+1)}
		for j := 0; j < n; j++ {
			jc.P = append(jc.P, c.P[j])
		}
		jc.P = append(jc.P, tc.P)
		if tc.I != nil {
			jc.SI = tc.I
		}
		if tc.D != nil {
			jc.SD = tc.D
		}
		Append(dest, jc)
	}
	return nil
}

// TransformComponent mirrors json.transformComponent (oracle L289-580). It
// transforms one component c against one otherC, appending result
// component(s) to dest (nothing appended = no-op).
func TransformComponent(dest *[]Component, c, otherC Component, side string) error {
	c = cloneComponent(c)
	otherC = cloneComponent(otherC)

	// Phantom 0 for the na case (the "icky path hax"). common/common2/cpl are
	// computed with phantom segments in place; paths are restored afterwards.
	if c.NA != nil {
		c.P = append(c.P, 0)
	}
	if otherC.NA != nil {
		otherC.P = append(otherC.P, 0)
	}

	common, commonOK := CommonPath(c.P, otherC.P)
	common2, common2OK := CommonPath(otherC.P, c.P)

	cpl := len(c.P)
	otherCpl := len(otherC.P)

	if c.NA != nil {
		c.P = c.P[:len(c.P)-1]
	}
	if otherC.NA != nil {
		otherC.P = otherC.P[:len(otherC.P)-1]
	}

	// A-branch (oracle L305-325): handled here due to the icky path hax.
	// Vendored truthiness check `if (otherC.na)`: na==0 does not enter.
	if otherC.NA != nil && *otherC.NA != 0 {
		if common2OK && otherCpl >= cpl && segEq(segAt(otherC.P, common2), segAt(c.P, common2)) {
			oc := cloneComponent(otherC)
			oc.P = sliceSafe(oc.P, cpl)
			if c.LD != nil {
				v := *c.LD
				if err := innerApply(&v, oc); err != nil {
					return err
				}
				c.LD = &v
			} else if c.OD != nil {
				v := *c.OD
				if err := innerApply(&v, oc); err != nil {
					return err
				}
				c.OD = &v
			}
		}
		Append(dest, c)
		return nil
	}

	// B-branch (oracle L342-365): transform based on c.
	if common2OK && otherCpl > cpl && segEq(segAt(c.P, common2), segAt(otherC.P, common2)) {
		oc := cloneComponent(otherC)
		oc.P = sliceSafe(oc.P, cpl)
		if c.LD != nil {
			v := *c.LD
			if err := innerApply(&v, oc); err != nil {
				return err
			}
			c.LD = &v
		} else if c.OD != nil {
			v := *c.OD
			if err := innerApply(&v, oc); err != nil {
				return err
			}
			c.OD = &v
		}
	}

	if commonOK || common == -1 {
		// C-branch (oracle L369-580): transform based on otherC.
		// JS `common != null`: the -1 sentinel also passes.
		commonOperand := cpl == otherCpl
		_ = common2

		if otherC.NA != nil {
			// Handled above due to the icky path hax (the na==0 case falls
			// through with no otherC-specific mutation; append below).
		} else if otherC.SI != nil || otherC.SD != nil {
			if c.SI != nil || c.SD != nil {
				if !commonOperand {
					return ErrMustBeString
				}
				return textBridge(dest, c, otherC, common, side)
			}
		} else if otherC.LI != nil && otherC.LD != nil {
			if segEq(segAt(c.P, common), segAt(otherC.P, common)) {
				if !commonOperand {
					return nil
				} else if c.LD != nil {
					if c.LI != nil && side == "left" {
						v := cloneVal(*otherC.LI)
						c.LD = &v
					} else {
						return nil
					}
				}
			}
		} else if otherC.LI != nil {
			if c.LI != nil && c.LD == nil && commonOperand && segEq(segAt(c.P, common), segAt(otherC.P, common)) {
				if side == "right" {
					segInc(c.P, common)
				}
			} else if segLe(otherC.P[common], c.P[common]) {
				segInc(c.P, common)
			}
			if c.LM != nil {
				if commonOperand {
					if n, ok := segIntSafe(otherC.P, common); ok && n <= *c.LM {
						*c.LM++
					}
				}
			}
		} else if otherC.LD != nil {
			if c.LM != nil {
				if commonOperand {
					if segEq(segAt(c.P, common), segAt(otherC.P, common)) {
						return nil
					}
					p, _ := segIntSafe(otherC.P, common)
					from, _ := segIntSafe(c.P, common)
					to := *c.LM
					if p < to || (p == to && from < to) {
						*c.LM--
					}
				}
			}
			if segLt(otherC.P[common], c.P[common]) {
				segDec(c.P, common)
			} else if segEq(segAt(c.P, common), segAt(otherC.P, common)) {
				if otherCpl < cpl {
					return nil
				}
				if c.LD != nil {
					if c.LI != nil {
						c.LD = nil
					} else {
						return nil
					}
				}
			}
		} else if otherC.LM != nil {
			if c.LM != nil && cpl == otherCpl {
				from, _ := segIntSafe(c.P, common)
				to := *c.LM
				otherFrom, _ := segIntSafe(otherC.P, common)
				otherTo := *otherC.LM
				if otherFrom != otherTo {
					if from == otherFrom {
						// They moved it: tie-break on side.
						if side == "left" {
							segSet(c.P, common, otherTo)
							if from == to {
								c.LM = otherC.LM
							}
						} else {
							return nil
						}
					} else {
						// They moved around it.
						if from > otherFrom {
							segDec(c.P, common)
						}
						if from > otherTo {
							segInc(c.P, common)
						} else if from == otherTo {
							if otherFrom > otherTo {
								segInc(c.P, common)
								if from == to {
									*c.LM++
								}
							}
						}
						if to > otherFrom {
							*c.LM--
						} else if to == otherFrom {
							if to > from {
								*c.LM--
							}
						}
						if to > otherTo {
							*c.LM++
						} else if to == otherTo {
							if (otherTo > otherFrom && to > from) || (otherTo < otherFrom && to < from) {
								if side == "right" {
									*c.LM++
								}
							} else {
								if to > from {
									*c.LM++
								} else if to == otherFrom {
									*c.LM--
								}
							}
						}
					}
				}
			} else if c.LI != nil && c.LD == nil && commonOperand {
				from, _ := segIntSafe(otherC.P, common)
				to := *otherC.LM
				p, _ := segIntSafe(c.P, common)
				if p > from {
					segDec(c.P, common)
				}
				if p > to {
					segInc(c.P, common)
				}
			} else {
				from, _ := segIntSafe(otherC.P, common)
				to := *otherC.LM
				p, _ := segIntSafe(c.P, common)
				if p == from {
					segSet(c.P, common, to)
				} else {
					if p > from {
						segDec(c.P, common)
					}
					if p > to {
						segInc(c.P, common)
					} else if p == to {
						if from > to {
							segInc(c.P, common)
						}
					}
				}
			}
		} else if otherC.OI != nil && otherC.OD != nil {
			if segEq(segAt(c.P, common), segAt(otherC.P, common)) {
				if c.OI != nil && commonOperand {
					if side == "right" {
						return nil
					}
					c.OD = otherC.OI
				} else {
					return nil
				}
			}
		} else if otherC.OI != nil {
			if c.OI != nil && segEq(segAt(c.P, common), segAt(otherC.P, common)) {
				// Left wins if we try to insert at the same place: we make our
				// op a replace (od) and fall through to the final append, where
				// append() merges it into {od, oi}.
				if side != "left" {
					return nil
				}
				Append(dest, Component{P: c.P, OD: otherC.OI})
				// fall through to the final Append(dest, c).
			}
		} else if otherC.OD != nil {
			if segEq(segAt(c.P, common), segAt(otherC.P, common)) {
				if !commonOperand {
					return nil
				}
				if c.OI != nil {
					// Vendored `delete c.od` (faithful port of the bug: the
					// od field is cleared, not oi).
					c.OD = nil
				} else {
					return nil
				}
			}
		}
	}

	Append(dest, c)
	return nil
}

func innerApply(target *any, oc Component) error {
	res, err := Apply(cloneVal(*target), []Component{oc})
	if err != nil {
		return err
	}
	*target = res
	return nil
}

// TransformComponentX mirrors the bootstrap transformComponentX
// (helpers.js L28-31).
func TransformComponentX(left, right Component, destLeft, destRight *[]Component) error {
	if err := TransformComponent(destLeft, left, right, "left"); err != nil {
		return err
	}
	return TransformComponent(destRight, right, left, "right")
}

// TransformX mirrors the bootstrap transformX (helpers.js L34-83).
func TransformX(leftOp, rightOp []Component) ([]Component, []Component, error) {
	CheckValidOp(leftOp)
	CheckValidOp(rightOp)

	newRightOp := []Component{}
	for i := range rightOp {
		rightComponent := rightOp[i]
		rcNull := false
		newLeftOp := []Component{}
		k := 0
		for k < len(leftOp) {
			nextC := []Component{}
			if err := TransformComponentX(leftOp[k], rightComponent, &newLeftOp, &nextC); err != nil {
				return nil, nil, err
			}
			k++
			if len(nextC) == 1 {
				rightComponent = nextC[0]
			} else if len(nextC) == 0 {
				for j := k; j < len(leftOp); j++ {
					Append(&newLeftOp, leftOp[j])
				}
				rcNull = true
				break
			} else {
				lSub, rSub, err := TransformX(leftOp[k:], nextC)
				if err != nil {
					return nil, nil, err
				}
				for j := range lSub {
					Append(&newLeftOp, lSub[j])
				}
				for j := range rSub {
					Append(&newRightOp, rSub[j])
				}
				rcNull = true
				break
			}
		}
		if !rcNull {
			Append(&newRightOp, rightComponent)
		}
		leftOp = newLeftOp
	}
	return leftOp, newRightOp, nil
}

// Transform mirrors the bootstrap type.transform (helpers.js L86-111).
func Transform(op, otherOp []Component, side string) ([]Component, error) {
	if side != "left" && side != "right" {
		return nil, ErrBadSide
	}
	if len(otherOp) == 0 {
		return op, nil
	}
	if len(op) == 1 && len(otherOp) == 1 {
		out := []Component{}
		if err := TransformComponent(&out, op[0], otherOp[0], side); err != nil {
			return nil, err
		}
		return out, nil
	}
	if side == "left" {
		left, _, err := TransformX(op, otherOp)
		return left, err
	}
	_, right, err := TransformX(otherOp, op)
	return right, err
}

func sliceSafe(p []any, i int) []any {
	if i < 0 || i >= len(p) {
		return []any{}
	}
	return p[i:]
}
