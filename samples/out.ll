%math.linal.mat.Matrix = type { i32, i32 }
%math.linal.vec.Vec2 = type { double, double }

declare void @printf(i8*, ...)

define %math.linal.mat.Matrix @math.linal.mat.Matrix_op_add(%math.linal.mat.Matrix* %self, %math.linal.mat.Matrix* %other) {
entry:
  %self.addr = alloca %math.linal.mat.Matrix*
  store %math.linal.mat.Matrix* %self, %math.linal.mat.Matrix** %self.addr
  %other.addr = alloca %math.linal.mat.Matrix*
  store %math.linal.mat.Matrix* %other, %math.linal.mat.Matrix** %other.addr
  %1 = alloca %math.linal.mat.Matrix
  %2 = load %math.linal.mat.Matrix*, %math.linal.mat.Matrix** %self.addr
  %3 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %2, i32 0, i32 0
  %4 = load i32, i32* %3
  %5 = load %math.linal.mat.Matrix*, %math.linal.mat.Matrix** %other.addr
  %6 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %5, i32 0, i32 0
  %7 = load i32, i32* %6
  %8 = add i32 %4, %7
  %9 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %1, i32 0, i32 0
  store i32 %8, i32* %9
  %10 = load %math.linal.mat.Matrix*, %math.linal.mat.Matrix** %self.addr
  %11 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %10, i32 0, i32 1
  %12 = load i32, i32* %11
  %13 = load %math.linal.mat.Matrix*, %math.linal.mat.Matrix** %other.addr
  %14 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %13, i32 0, i32 1
  %15 = load i32, i32* %14
  %16 = add i32 %12, %15
  %17 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %1, i32 0, i32 1
  store i32 %16, i32* %17
  %18 = load %math.linal.mat.Matrix, %math.linal.mat.Matrix* %1
  ret %math.linal.mat.Matrix %18
}

define void @scale(%math.linal.mat.Matrix* %self, i32 %factor) {
entry:
  %self.addr = alloca %math.linal.mat.Matrix*
  store %math.linal.mat.Matrix* %self, %math.linal.mat.Matrix** %self.addr
  %factor.addr = alloca i32
  store i32 %factor, i32* %factor.addr
  %1 = load %math.linal.mat.Matrix*, %math.linal.mat.Matrix** %self.addr
  %2 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %1, i32 0, i32 0
  %3 = load i32, i32* %factor.addr
  %4 = load i32, i32* %2
  %5 = mul i32 %4, %3
  store i32 %5, i32* %2
  %6 = load %math.linal.mat.Matrix*, %math.linal.mat.Matrix** %self.addr
  %7 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %6, i32 0, i32 1
  %8 = load i32, i32* %factor.addr
  %9 = load i32, i32* %7
  %10 = mul i32 %9, %8
  store i32 %10, i32* %7
  ret void
}

define %math.linal.vec.Vec2 @math.linal.vec.Vec2_op_add(%math.linal.vec.Vec2* %self, %math.linal.vec.Vec2* %other) {
entry:
  %self.addr = alloca %math.linal.vec.Vec2*
  store %math.linal.vec.Vec2* %self, %math.linal.vec.Vec2** %self.addr
  %other.addr = alloca %math.linal.vec.Vec2*
  store %math.linal.vec.Vec2* %other, %math.linal.vec.Vec2** %other.addr
  %1 = alloca %math.linal.vec.Vec2
  %2 = load %math.linal.vec.Vec2*, %math.linal.vec.Vec2** %self.addr
  %3 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %2, i32 0, i32 0
  %4 = load double, double* %3
  %5 = load %math.linal.vec.Vec2*, %math.linal.vec.Vec2** %other.addr
  %6 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %5, i32 0, i32 0
  %7 = load double, double* %6
  %8 = fadd double %4, %7
  %9 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %1, i32 0, i32 0
  store double %8, double* %9
  %10 = load %math.linal.vec.Vec2*, %math.linal.vec.Vec2** %self.addr
  %11 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %10, i32 0, i32 1
  %12 = load double, double* %11
  %13 = load %math.linal.vec.Vec2*, %math.linal.vec.Vec2** %other.addr
  %14 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %13, i32 0, i32 1
  %15 = load double, double* %14
  %16 = fadd double %12, %15
  %17 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %1, i32 0, i32 1
  store double %16, double* %17
  %18 = load %math.linal.vec.Vec2, %math.linal.vec.Vec2* %1
  ret %math.linal.vec.Vec2 %18
}

define double @math.linal.vec.Vec2_op_mul(%math.linal.vec.Vec2* %self, %math.linal.vec.Vec2* %vec) {
entry:
  %self.addr = alloca %math.linal.vec.Vec2*
  store %math.linal.vec.Vec2* %self, %math.linal.vec.Vec2** %self.addr
  %vec.addr = alloca %math.linal.vec.Vec2*
  store %math.linal.vec.Vec2* %vec, %math.linal.vec.Vec2** %vec.addr
  %1 = load %math.linal.vec.Vec2*, %math.linal.vec.Vec2** %self.addr
  %2 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %1, i32 0, i32 0
  %3 = load double, double* %2
  %4 = load %math.linal.vec.Vec2*, %math.linal.vec.Vec2** %vec.addr
  %5 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %4, i32 0, i32 0
  %6 = load double, double* %5
  %7 = fmul double %3, %6
  %8 = load %math.linal.vec.Vec2*, %math.linal.vec.Vec2** %self.addr
  %9 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %8, i32 0, i32 1
  %10 = load double, double* %9
  %11 = load %math.linal.vec.Vec2*, %math.linal.vec.Vec2** %vec.addr
  %12 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %11, i32 0, i32 1
  %13 = load double, double* %12
  %14 = fmul double %10, %13
  %15 = fadd double %7, %14
  ret double %15
}

define i32 @main() {
entry:
  %1 = getelementptr inbounds [26 x i8], [26 x i8]* @.str.1, i64 0, i64 0
  call void @printf(i8* %1)
  %m1_2 = alloca %math.linal.mat.Matrix
  %3 = alloca %math.linal.mat.Matrix
  %4 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %3, i32 0, i32 0
  store i32 10, i32* %4
  %5 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %3, i32 0, i32 1
  store i32 20, i32* %5
  %6 = load %math.linal.mat.Matrix, %math.linal.mat.Matrix* %3
  store %math.linal.mat.Matrix %6, %math.linal.mat.Matrix* %m1_2
  %m2_7 = alloca %math.linal.mat.Matrix
  %8 = alloca %math.linal.mat.Matrix
  %9 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %8, i32 0, i32 0
  store i32 5, i32* %9
  %10 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %8, i32 0, i32 1
  store i32 5, i32* %10
  %11 = load %math.linal.mat.Matrix, %math.linal.mat.Matrix* %8
  store %math.linal.mat.Matrix %11, %math.linal.mat.Matrix* %m2_7
  %sum_12 = alloca %math.linal.mat.Matrix
  %13 = call %math.linal.mat.Matrix @math.linal.mat.Matrix_op_add(%math.linal.mat.Matrix* %m1_2, %math.linal.mat.Matrix* %m2_7)
  store %math.linal.mat.Matrix %13, %math.linal.mat.Matrix* %sum_12
  call void @scale(%math.linal.mat.Matrix* %sum_12, i32 2)
  %14 = getelementptr inbounds [43 x i8], [43 x i8]* @.str.2, i64 0, i64 0
  %15 = load %math.linal.mat.Matrix, %math.linal.mat.Matrix* %sum_12
  %16 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %sum_12, i32 0, i32 0
  %17 = load i32, i32* %16
  %18 = load %math.linal.mat.Matrix, %math.linal.mat.Matrix* %sum_12
  %19 = getelementptr inbounds %math.linal.mat.Matrix, %math.linal.mat.Matrix* %sum_12, i32 0, i32 1
  %20 = load i32, i32* %19
  call void @printf(i8* %14, i32 %17, i32 %20)
  %v1_21 = alloca %math.linal.vec.Vec2
  %22 = alloca %math.linal.vec.Vec2
  %23 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %22, i32 0, i32 0
  store double 10.5, double* %23
  %24 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %22, i32 0, i32 1
  store double 20.5, double* %24
  %25 = load %math.linal.vec.Vec2, %math.linal.vec.Vec2* %22
  store %math.linal.vec.Vec2 %25, %math.linal.vec.Vec2* %v1_21
  %v2_26 = alloca %math.linal.vec.Vec2
  %27 = alloca %math.linal.vec.Vec2
  %28 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %27, i32 0, i32 0
  store double 5.0, double* %28
  %29 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %27, i32 0, i32 1
  store double 5.0, double* %29
  %30 = load %math.linal.vec.Vec2, %math.linal.vec.Vec2* %27
  store %math.linal.vec.Vec2 %30, %math.linal.vec.Vec2* %v2_26
  %dot_31 = alloca double
  %32 = call double @math.linal.vec.Vec2_op_mul(%math.linal.vec.Vec2* %v1_21, %math.linal.vec.Vec2* %v2_26)
  store double %32, double* %dot_31
  %33 = getelementptr inbounds [17 x i8], [17 x i8]* @.str.3, i64 0, i64 0
  %34 = load %math.linal.vec.Vec2, %math.linal.vec.Vec2* %v1_21
  %35 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %v1_21, i32 0, i32 0
  %36 = load double, double* %35
  %37 = load %math.linal.vec.Vec2, %math.linal.vec.Vec2* %v1_21
  %38 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %v1_21, i32 0, i32 1
  %39 = load double, double* %38
  call void @printf(i8* %33, double %36, double %39)
  %40 = getelementptr inbounds [17 x i8], [17 x i8]* @.str.4, i64 0, i64 0
  %41 = load %math.linal.vec.Vec2, %math.linal.vec.Vec2* %v2_26
  %42 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %v2_26, i32 0, i32 0
  %43 = load double, double* %42
  %44 = load %math.linal.vec.Vec2, %math.linal.vec.Vec2* %v2_26
  %45 = getelementptr inbounds %math.linal.vec.Vec2, %math.linal.vec.Vec2* %v2_26, i32 0, i32 1
  %46 = load double, double* %45
  call void @printf(i8* %40, double %43, double %46)
  %47 = getelementptr inbounds [17 x i8], [17 x i8]* @.str.5, i64 0, i64 0
  %48 = load double, double* %dot_31
  call void @printf(i8* %47, double %48)
  %49 = getelementptr inbounds [39 x i8], [39 x i8]* @.str.6, i64 0, i64 0
  call void @printf(i8* %49)
  ret i32 0
}

@.str.1 = private unnamed_addr constant [26 x i8] c"Booting main_test.syn...\0A\00"
@.str.2 = private unnamed_addr constant [43 x i8] c"Dynamically Loaded Matrix Sum: a=%d, b=%d\0A\00"
@.str.3 = private unnamed_addr constant [17 x i8] c"Vector 1 %f, %f\0A\00"
@.str.4 = private unnamed_addr constant [17 x i8] c"Vector 2 %f, %f\0A\00"
@.str.5 = private unnamed_addr constant [17 x i8] c"Dot Product: %f\0A\00"
@.str.6 = private unnamed_addr constant [39 x i8] c"--- DAG Auto-Loader Test Complete ---\0A\00"
