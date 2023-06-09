package samepermutationargument

import (
	"fmt"
	"io"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	"github.com/jsign/curdleproofs/common"
	"github.com/jsign/curdleproofs/grandproductargument"
	"github.com/jsign/curdleproofs/msmaccumulator"
	"github.com/jsign/curdleproofs/transcript"
)

var (
	labelStep1 = []byte("same_perm_step1")
	labelAlpha = []byte("same_perm_alpha")
	labelBeta  = []byte("same_perm_beta")
)

type CRS struct {
	Gs []bls12381.G1Affine
	Hs []bls12381.G1Affine
	H  bls12381.G1Jac
}

type Proof struct {
	B        bls12381.G1Jac
	gpaProof grandproductargument.Proof
}

func Prove(
	crs CRS,
	A bls12381.G1Jac,
	M bls12381.G1Jac,
	as []fr.Element,
	permutation []uint32,
	rs_a []fr.Element,
	rs_m []fr.Element,
	transcript *transcript.Transcript,
	rand *common.Rand,
) (Proof, error) {
	// Step 1
	transcript.AppendPoints(labelStep1, A, M)
	transcript.AppendScalars(labelStep1, as...)
	alpha := transcript.GetAndAppendChallenge(labelAlpha)
	beta := transcript.GetAndAppendChallenge(labelBeta)

	// Step 2
	permutedAs := common.Permute(as, permutation)
	permutationFrs := make([]fr.Element, len(permutation))
	for i := range permutation {
		permutationFrs[i] = fr.NewElement(uint64(permutation[i]))
	}
	bs := make([]fr.Element, len(as))
	p := fr.One()
	for i := range permutedAs {
		bs[i].Mul(&alpha, &permutationFrs[i]).Add(&bs[i], &permutedAs[i]).Add(&bs[i], &beta)
		p.Mul(&p, &bs[i])
	}

	betas := make([]fr.Element, len(crs.Gs))
	for i := range betas {
		betas[i] = beta
	}
	var msmBetasGs bls12381.G1Jac
	if _, err := msmBetasGs.MultiExp(crs.Gs, betas, common.MultiExpConf); err != nil {
		return Proof{}, fmt.Errorf("failed to compute msm(Bs, Gs): %s", err)
	}
	var alphaM bls12381.G1Jac
	alphaM.ScalarMultiplication(&M, common.FrToBigInt(&alpha))
	var B bls12381.G1Jac
	B.Set(&A).AddAssign(&alphaM).AddAssign(&msmBetasGs)

	rs_b := make([]fr.Element, len(rs_a))
	for i := range rs_b {
		rs_b[i].Mul(&alpha, &rs_m[i]).Add(&rs_b[i], &rs_a[i])
	}

	gpaproof, err := grandproductargument.Prove(
		grandproductargument.CRS{
			Gs: crs.Gs,
			Hs: crs.Hs,
			H:  crs.H,
		},
		B,
		p,
		bs,
		rs_b,
		transcript,
		rand,
	)
	if err != nil {
		return Proof{}, fmt.Errorf("failed to prove grand product argument: %s", err)
	}

	return Proof{
		B:        B,
		gpaProof: gpaproof,
	}, nil
}

func Verify(
	proof Proof,
	crs CRS,
	Gsum bls12381.G1Affine,
	Hsum bls12381.G1Affine,
	A bls12381.G1Jac,
	M bls12381.G1Jac,
	as []fr.Element,
	numBlinders int,
	transcript *transcript.Transcript,
	msmAccumulator *msmaccumulator.MsmAccumulator,

	rand *common.Rand,
) (bool, error) {
	// Step 1
	// TODO(jsign): double check FS since doesn't seem to match paper.
	transcript.AppendPoints(labelStep1, A, M)
	transcript.AppendScalars(labelStep1, as...)
	alpha := transcript.GetAndAppendChallenge(labelAlpha)
	beta := transcript.GetAndAppendChallenge(labelBeta)

	// Step 2
	p := fr.One()
	for i := range as {
		tmp := fr.NewElement(uint64(i))
		tmp.Mul(&tmp, &alpha).Add(&tmp, &beta).Add(&tmp, &as[i])
		p.Mul(&p, &tmp)
	}

	betas := make([]fr.Element, len(crs.Gs))
	for i := range betas {
		betas[i] = beta
	}
	var C bls12381.G1Jac
	var alphaM bls12381.G1Jac
	alphaM.ScalarMultiplication(&M, common.FrToBigInt(&alpha))
	C.Set(&proof.B).SubAssign(&A).SubAssign(&alphaM)
	if err := msmAccumulator.AccumulateCheck(C, betas, crs.Gs, rand); err != nil {
		return false, fmt.Errorf("failed to accumulate check: %s", err)
	}

	ok, err := grandproductargument.Verify(
		proof.gpaProof,
		grandproductargument.CRS{
			Gs: crs.Gs,
			Hs: crs.Hs,
			H:  crs.H,
		},
		Gsum,
		Hsum,
		proof.B,
		p,
		numBlinders,
		transcript,
		msmAccumulator,
		rand,
	)
	if err != nil {
		return false, fmt.Errorf("failed to verify grand product argument: %s", err)
	}
	return ok, nil
}

func (p *Proof) FromReader(r io.Reader) error {
	var tmp bls12381.G1Affine
	d := bls12381.NewDecoder(r)

	if err := d.Decode(&tmp); err != nil {
		return fmt.Errorf("failed to decode B: %s", err)
	}
	p.B.FromAffine(&tmp)

	if err := p.gpaProof.FromReader(r); err != nil {
		return fmt.Errorf("failed to decode GPA proof: %s", err)
	}
	return nil
}

func (p *Proof) Serialize(w io.Writer) error {
	e := bls12381.NewEncoder(w)
	var bAffine bls12381.G1Affine
	bAffine.FromJacobian(&p.B)
	if err := e.Encode(&bAffine); err != nil {
		return fmt.Errorf("failed to encode B: %s", err)
	}
	if err := p.gpaProof.Serialize(w); err != nil {
		return fmt.Errorf("failed to encode GPA proof: %s", err)
	}
	return nil
}
