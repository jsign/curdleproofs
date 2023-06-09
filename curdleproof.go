package curdleproof

import (
	"fmt"
	"io"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	"github.com/jsign/curdleproofs/common"
	"github.com/jsign/curdleproofs/groupcommitment"
	"github.com/jsign/curdleproofs/msmaccumulator"
	"github.com/jsign/curdleproofs/samemultiscalarargument"
	"github.com/jsign/curdleproofs/samepermutationargument"
	"github.com/jsign/curdleproofs/samescalarargument"
	"github.com/jsign/curdleproofs/transcript"
)

var (
	labelTranscript = []byte("curdleproofs")
	labelStep1      = []byte("curdleproofs_step1")
	labelVecA       = []byte("curdleproofs_vec_a")

	zeroPoint = bls12381.G1Affine{}
	zeroFr    = fr.Element{}
)

type Proof struct {
	A                    bls12381.G1Jac
	T                    groupcommitment.GroupCommitment
	U                    groupcommitment.GroupCommitment
	R                    bls12381.G1Jac
	S                    bls12381.G1Jac
	proofSamePermutation samepermutationargument.Proof
	proofSameScalar      samescalarargument.Proof
	proofSameMultiscalar samemultiscalarargument.Proof
}

func Prove(
	crs CRS,
	Rs []bls12381.G1Affine,
	Ss []bls12381.G1Affine,
	Ts []bls12381.G1Affine,
	Us []bls12381.G1Affine,
	M bls12381.G1Jac,
	perm []uint32,
	k fr.Element,
	rs_m []fr.Element,
	rand *common.Rand,
) (Proof, error) {
	transcript := transcript.New(labelTranscript)

	// Step 1
	transcript.AppendPointsAffine(labelStep1, Rs...)
	transcript.AppendPointsAffine(labelStep1, Ss...)
	transcript.AppendPointsAffine(labelStep1, Ts...)
	transcript.AppendPointsAffine(labelStep1, Us...)
	transcript.AppendPoints(labelStep1, M)
	as := transcript.GetAndAppendChallenges(labelVecA, len(Rs))

	// Step 2
	rs_a, err := rand.GetFrs(common.N_BLINDERS - 2)
	if err != nil {
		return Proof{}, fmt.Errorf("getting rs_a: %s", err)
	}

	rs_a_prime := make([]fr.Element, 0, len(rs_a)+1+1)
	rs_a_prime = append(rs_a_prime, rs_a...)
	rs_a_prime = append(rs_a_prime, zeroFr, zeroFr)

	perm_as := common.Permute(as, perm)

	var A, A_L, A_R bls12381.G1Jac
	if _, err := A_L.MultiExp(crs.Gs, perm_as, common.MultiExpConf); err != nil {
		return Proof{}, fmt.Errorf("computing A_L: %s", err)
	}
	if _, err := A_R.MultiExp(crs.Hs, rs_a_prime, common.MultiExpConf); err != nil {
		return Proof{}, fmt.Errorf("computing A_R: %s", err)
	}
	A.Set(&A_L).AddAssign(&A_R)

	proofSamePerm, err := samepermutationargument.Prove(
		samepermutationargument.CRS{
			Gs: crs.Gs,
			Hs: crs.Hs,
			H:  crs.H,
		},
		A,
		M,
		as,
		perm,
		rs_a_prime,
		rs_m,
		transcript,
		rand,
	)
	if err != nil {
		return Proof{}, fmt.Errorf("proving same permutation: %s", err)
	}

	// Step 3
	r_t, err := rand.GetFr()
	if err != nil {
		return Proof{}, fmt.Errorf("getting random r_t: %s", err)
	}
	r_u, err := rand.GetFr()
	if err != nil {
		return Proof{}, fmt.Errorf("getting random r_u: %s", err)
	}
	var R bls12381.G1Jac
	if _, err := R.MultiExp(Rs, as, common.MultiExpConf); err != nil {
		return Proof{}, fmt.Errorf("computing R: %s", err)
	}
	var S bls12381.G1Jac
	if _, err := S.MultiExp(Ss, as, common.MultiExpConf); err != nil {
		return Proof{}, fmt.Errorf("computing S: %s", err)
	}

	var tmp bls12381.G1Jac
	tmp.ScalarMultiplication(&R, common.FrToBigInt(&k))
	T := groupcommitment.New(crs.Gt, crs.H, tmp, r_t)
	tmp.ScalarMultiplication(&S, common.FrToBigInt(&k))
	U := groupcommitment.New(crs.Gu, crs.H, tmp, r_u)

	// TODO(jsign): enforce assumption in callees about mutation of parameters.
	proofSameScalar, err := samescalarargument.Prove(
		samescalarargument.CRS{
			Gt: crs.Gt,
			Gu: crs.Gu,
			H:  crs.H,
		},
		R,
		S,
		T,
		U,
		k,
		r_t,
		r_u,
		transcript,
		rand,
	)
	if err != nil {
		return Proof{}, fmt.Errorf("proving same scalar: %s", err)
	}

	// Step 4
	A_prime := A
	A_prime.AddAssign(&T.T_1)
	A_prime.AddAssign(&U.T_1)

	G := make([]bls12381.G1Affine, 0, len(crs.Gs)+(common.N_BLINDERS-2)+1+1)
	G = append(G, crs.Gs...)
	G = append(G, crs.Hs[:common.N_BLINDERS-2]...)
	gxaffine := bls12381.BatchJacobianToAffineG1([]bls12381.G1Jac{crs.Gt, crs.Gu})
	G = append(G, gxaffine...)

	T_prime := make([]bls12381.G1Affine, 0, len(Ts)+2+1+1)
	T_prime = append(T_prime, Ts...)
	var crsHAffine bls12381.G1Affine
	crsHAffine.FromJacobian(&crs.H)
	T_prime = append(T_prime, zeroPoint, zeroPoint, crsHAffine, zeroPoint)

	U_prime := make([]bls12381.G1Affine, 0, len(Us)+2+1+1)
	U_prime = append(U_prime, Us...)
	U_prime = append(U_prime, zeroPoint, zeroPoint, zeroPoint)
	U_prime = append(U_prime, crsHAffine)

	x := make([]fr.Element, 0, len(perm_as)+len(rs_a)+1+1)
	x = append(x, perm_as...)
	x = append(x, rs_a...)
	x = append(x, r_t, r_u)

	proofSameMultiscalar, err := samemultiscalarargument.Prove(
		G,
		A_prime,
		T.T_2,
		U.T_2,
		T_prime,
		U_prime,
		x,
		transcript,
		rand,
	)
	if err != nil {
		return Proof{}, fmt.Errorf("proving same multiscalar: %s", err)
	}

	return Proof{
		A,
		T,
		U,
		R,
		S,
		proofSamePerm,
		proofSameScalar,
		proofSameMultiscalar,
	}, nil
}

func Verify(
	proof Proof,
	crs CRS,
	Rs []bls12381.G1Affine,
	Ss []bls12381.G1Affine,
	Ts []bls12381.G1Affine,
	Us []bls12381.G1Affine,
	M bls12381.G1Jac,
	rand *common.Rand,
) (bool, error) {
	transcript := transcript.New(labelTranscript)
	msmAccumulator := msmaccumulator.New()

	// Make sure that randomizer was not the zero element (and wiped out the ciphertexts)
	if Ts[0].IsInfinity() {
		return false, fmt.Errorf("randomizer is zero")
	}

	// Step 1
	transcript.AppendPointsAffine(labelStep1, Rs...)
	transcript.AppendPointsAffine(labelStep1, Ss...)
	transcript.AppendPointsAffine(labelStep1, Ts...)
	transcript.AppendPointsAffine(labelStep1, Us...)
	transcript.AppendPoints(labelStep1, M)
	as := transcript.GetAndAppendChallenges(labelVecA, len(Rs))

	// Step 2
	ok, err := samepermutationargument.Verify(
		proof.proofSamePermutation,
		samepermutationargument.CRS{
			Gs: crs.Gs,
			Hs: crs.Hs,
			H:  crs.H,
		},
		crs.Gsum,
		crs.Hsum,
		proof.A,
		M,
		as,
		common.N_BLINDERS,
		transcript,
		msmAccumulator,
		rand,
	)
	if err != nil {
		return false, fmt.Errorf("verifying same permutation: %s", err)
	}
	if !ok {
		return false, nil
	}

	// Step 3
	if ok := samescalarargument.Verify(
		proof.proofSameScalar,
		samescalarargument.CRS{
			Gt: crs.Gt,
			Gu: crs.Gu,
			H:  crs.H,
		},
		proof.R,
		proof.S,
		proof.T,
		proof.U,
		transcript,
	); !ok {
		return false, nil
	}

	// Step 4
	Aprime := proof.A
	Aprime.AddAssign(&proof.T.T_1).AddAssign(&proof.U.T_1)

	Gs := make([]bls12381.G1Affine, 0, len(crs.Gs)+(common.N_BLINDERS-2)+1+1)
	Gs = append(Gs, crs.Gs...)
	Gs = append(Gs, crs.Hs[:common.N_BLINDERS-2]...)
	gaffs := bls12381.BatchJacobianToAffineG1([]bls12381.G1Jac{crs.Gt, crs.Gu})
	Gs = append(Gs, gaffs...)

	Tsprime := make([]bls12381.G1Affine, 0, len(Ts)+2+1+1)
	Tsprime = append(Tsprime, Ts...)
	var HAff bls12381.G1Affine
	HAff.FromJacobian(&crs.H)
	Tsprime = append(Tsprime, zeroPoint, zeroPoint, HAff, zeroPoint)

	Usprime := make([]bls12381.G1Affine, 0, len(Us)+2+1+1)
	Usprime = append(Usprime, Us...)
	Usprime = append(Usprime, zeroPoint, zeroPoint, zeroPoint, HAff)

	ok, err = samemultiscalarargument.Verify(
		proof.proofSameMultiscalar,
		Gs,
		Aprime,
		proof.T.T_2,
		proof.U.T_2,
		Tsprime,
		Usprime,
		transcript,
		msmAccumulator,
		rand,
	)
	if err != nil {
		return false, fmt.Errorf("verifying same multiscalar: %s", err)
	}
	if !ok {
		return false, nil
	}

	if err := msmAccumulator.AccumulateCheck(proof.R, as, Rs, rand); err != nil {
		return false, fmt.Errorf("msm accumulator check R, as, Rs: %s", err)
	}
	if err := msmAccumulator.AccumulateCheck(proof.S, as, Ss, rand); err != nil {
		return false, fmt.Errorf("msm accumulator check S, as, Ss: %s", err)
	}

	ok, err = msmAccumulator.Verify()
	if err != nil {
		return false, fmt.Errorf("verifying msm accumulator: %s", err)
	}
	return ok, nil
}

func (p *Proof) FromReader(r io.Reader) error {
	var tmp bls12381.G1Affine
	d := bls12381.NewDecoder(r)

	if err := d.Decode(&tmp); err != nil {
		return fmt.Errorf("decoding A: %s", err)
	}
	p.A.FromAffine(&tmp)

	if err := p.T.FromReader(r); err != nil {
		return fmt.Errorf("decoding T: %s", err)
	}
	if err := p.U.FromReader(r); err != nil {
		return fmt.Errorf("decoding U: %s", err)
	}
	if err := d.Decode(&tmp); err != nil {
		return fmt.Errorf("decoding R: %s", err)
	}
	p.R.FromAffine(&tmp)

	if err := d.Decode(&tmp); err != nil {
		return fmt.Errorf("decoding S: %s", err)
	}
	p.S.FromAffine(&tmp)

	if err := p.proofSamePermutation.FromReader(r); err != nil {
		return fmt.Errorf("decoding proofSamePermutation: %s", err)
	}
	if err := p.proofSameScalar.FromReader(r); err != nil {
		return fmt.Errorf("decoding proofSameScalar: %s", err)
	}
	if err := p.proofSameMultiscalar.FromReader(r); err != nil {
		return fmt.Errorf("decoding proofSameMultiscalar: %s", err)
	}

	return nil
}

func (p *Proof) Serialize(w io.Writer) error {
	e := bls12381.NewEncoder(w)
	ars := bls12381.BatchJacobianToAffineG1([]bls12381.G1Jac{p.A, p.R, p.S})
	if err := e.Encode(&ars[0]); err != nil {
		panic(fmt.Errorf("encoding A: %s", err))
	}
	if err := p.T.Serialize(w); err != nil {
		return fmt.Errorf("encoding T: %s", err)
	}
	if err := p.U.Serialize(w); err != nil {
		return fmt.Errorf("encoding U: %s", err)
	}
	if err := e.Encode(&ars[1]); err != nil {
		return fmt.Errorf("encoding R: %s", err)
	}
	if err := e.Encode(&ars[2]); err != nil {
		return fmt.Errorf("encoding S: %s", err)
	}
	if err := p.proofSamePermutation.Serialize(w); err != nil {
		return fmt.Errorf("encoding proofSamePermutation: %s", err)
	}
	if err := p.proofSameScalar.Serialize(w); err != nil {
		return fmt.Errorf("encoding proofSameScalar: %s", err)
	}
	if err := p.proofSameMultiscalar.Serialize(w); err != nil {
		return fmt.Errorf("encoding proofSameMultiscalar: %s", err)
	}

	return nil
}
