package samepermutationargument

import (
	"fmt"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	"github.com/jsign/curdleproofs/common"
	"github.com/jsign/curdleproofs/grandproductargument"
	"github.com/jsign/curdleproofs/transcript"
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
	crs *CRS,
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
	transcript.AppendPoints([]byte("same_perm_step1"), &A, &M)
	transcript.AppendScalars([]byte("same_perm_step1"), as...)
	alpha := transcript.GetAndAppendChallenge([]byte("same_perm_alpha"))
	beta := transcript.GetAndAppendChallenge([]byte("same_perm_beta"))

	// Step 2
	permutedAs := common.Permute(as, permutation)
	b := make([]fr.Element, len(as))
	p := fr.One()
	for i := range permutedAs {
		b[i].Mul(&alpha, &as[i]).Add(&b[i], &permutedAs[i]).Add(&b[i], &beta)
		p.Mul(&p, &b[i])
	}

	betas := make([]fr.Element, len(crs.Gs))
	for i := range betas {
		betas[i] = beta
	}
	var msmBsGs bls12381.G1Jac
	if _, err := msmBsGs.MultiExp(crs.Gs, betas, common.MultiExpConf); err != nil {
		return Proof{}, fmt.Errorf("failed to compute msm(Bs, Gs): %s", err)
	}
	var alphaM bls12381.G1Jac
	alphaM.ScalarMultiplication(&M, common.FrToBigInt(&alpha))
	var B bls12381.G1Jac
	B.Set(&A).AddAssign(&alphaM).AddAssign(&msmBsGs)

	rs_b := make([]fr.Element, len(rs_a))
	for i := range rs_b {
		rs_b[i].Mul(&alpha, &rs_m[i]).Add(&rs_b[i], &rs_a[i])
	}

	gpaproof, err := grandproductargument.Prove(
		&grandproductargument.CRS{
			Gs: crs.Gs,
			Hs: crs.Hs,
			H:  crs.H,
		},
		B,
		p,
		b,
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
